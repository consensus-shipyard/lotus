// Package mir implements ISS consensus protocol using the Mir protocol framework.
package mir

import (
	"bytes"
	"context"
	"crypto"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p-core/host"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	mircrypto "github.com/filecoin-project/mir/pkg/crypto"
	"github.com/filecoin-project/mir/pkg/eventlog"
	"github.com/filecoin-project/mir/pkg/eventmangler"
	"github.com/filecoin-project/mir/pkg/logging"
	"github.com/filecoin-project/mir/pkg/net"
	mirlibp2p "github.com/filecoin-project/mir/pkg/net/libp2p"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/simplewal"
	"github.com/filecoin-project/mir/pkg/systems/trantor"
	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/fifo"
	"github.com/filecoin-project/lotus/chain/consensus/mir/validator"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const (
	CheckpointDBKeyPrefix         = "mir/checkpoints/"
	InterceptorOutputEnv          = "MIR_INTERCEPTOR_OUTPUT"
	ManglerEnv                    = "MIR_MANGLER"
	ConfigurationRequestsDBPrefix = "mir/configuration/"
)

var (
	LatestCheckpointKey   = datastore.NewKey("mir/latest-check")
	LatestCheckpointPbKey = datastore.NewKey("mir/latest-check-pb")

	// SentConfigurationNumberKey is used to store SentConfigurationNumber
	// that is the maximum configuration request number that have been sent.
	SentConfigurationNumberKey = datastore.NewKey("mir/max-config-number")
	// ExecutedConfigurationNumberKey is used to store ExecutedConfigurationNumber
	// that is the maximum configuration request number that have been executed.
	ExecutedConfigurationNumberKey = datastore.NewKey("mir/min-config-number")
)

// Manager manages the Lotus and Mir nodes participating in ISS consensus protocol.
type Manager struct {
	ctx context.Context
	// Lotus types.
	NetName     dtypes.NetworkName
	ValidatorID address.Address
	Pool        *fifo.Pool

	// Mir types.
	MirNode       *mir.Node
	MirID         string
	WAL           *simplewal.WAL
	Net           net.Transport
	CryptoManager *CryptoManager
	StateManager  *StateManager
	interceptor   *eventlog.Recorder
	ToMir         chan chan []*mirproto.Request
	ds            db.DB
	stopCh        chan struct{}
	stopped       bool

	// Reconfiguration types.
	InitialValidatorSet  *validator.Set
	reconfigurationNonce uint64

	// Checkpoints
	segmentLength  int    // segment length determining the checkpoint period.
	checkpointRepo string // path where checkpoints are (optionally) persisted
}

func NewManager(ctx context.Context, validatorID address.Address, h host.Host, api v1api.FullNode, ds db.DB,
	membership validator.Reader, cfg *Config) (*Manager, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}

	initialValidatorSet, err := membership.GetValidatorSet()
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to get validator set: %w", validatorID, err)
	}
	if initialValidatorSet.Size() == 0 {
		return nil, fmt.Errorf("validator %v: empty validator set", validatorID)
	}

	_, initialMembership, err := validator.Membership(initialValidatorSet.Validators)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to build node membership: %w", validatorID, err)
	}

	mirID := validatorID.String()
	_, ok := initialMembership[t.NodeID(mirID)]
	if !ok {
		return nil, fmt.Errorf("validator %v failed to find its identity in membership", validatorID)
	}

	logger := newManagerLogger(mirID)

	// Create Mir modules.
	netTransport, err := mirlibp2p.NewTransport(mirlibp2p.DefaultParams(), h, t.NodeID(mirID), logger)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create network transport: %w", validatorID, err)
	}

	cryptoManager, err := NewCryptoManager(validatorID, api)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create crypto manager: %w", validatorID, err)
	}

	var interceptor *eventlog.Recorder
	interceptorOutput := os.Getenv(InterceptorOutputEnv)
	if interceptorOutput != "" {
		// TODO: Persist in repo path?
		log.Infof("Interceptor initialized")
		interceptor, err = eventlog.NewRecorder(
			t.NodeID(mirID),
			interceptorOutput,
			logging.Decorate(logger, "Interceptor: "),
		)
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to create interceptor: %w", validatorID, err)
		}
	}

	if cfg.SegmentLength < 0 {
		return nil, fmt.Errorf("validator %v segment length must not be negative", validatorID)
	}

	m := Manager{
		ctx:                 ctx,
		stopCh:              make(chan struct{}),
		ValidatorID:         validatorID,
		NetName:             netName,
		Pool:                fifo.New(),
		MirID:               mirID,
		interceptor:         interceptor,
		CryptoManager:       cryptoManager,
		Net:                 netTransport,
		ds:                  ds,
		InitialValidatorSet: initialValidatorSet,
		ToMir:               make(chan chan []*mirproto.Request),
		checkpointRepo:      cfg.CheckpointRepo,
		segmentLength:       cfg.SegmentLength,
	}

	m.StateManager, err = NewStateManager(ctx, initialMembership, &m, api)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to start mir state manager: %w", validatorID, err)
	}

	// Create SMR modules.
	mpool := pool.NewModule(
		m.ToMir,
		pool.DefaultModuleConfig(),
		pool.DefaultModuleParams(),
	)

	params := trantor.DefaultParams(initialMembership)
	params.Iss.SegmentLength = m.segmentLength
	params.Mempool.MaxTransactionsInBatch = 1024
	params.Iss.AdjustSpeed(1 * time.Second)
	params.Iss.ConfigOffset = ConfigOffset

	initCh := cfg.InitialCheckpoint
	// if no initial checkpoint provided in config
	if initCh == nil {
		initCh, err = m.initCheckpoint(params, 0)
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to get initial snapshot SMR system: %w", validatorID, err)
		}
	}

	smrSystem, err := trantor.New(
		t.NodeID(mirID),
		netTransport,
		initCh,
		m.CryptoManager,
		m.StateManager,
		params,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create SMR system: %w", validatorID, err)
	}

	smrSystem = smrSystem.
		WithModule("mempool", mpool).
		WithModule("hasher", mircrypto.NewHasher(crypto.SHA256)) // to use sha256 hash from cryptomodule.

	mirManglerParams := os.Getenv(ManglerEnv)
	if mirManglerParams != "" {
		p, err := GetEnvManglerParams()
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to get mangler params: %w", validatorID, err)
		}
		err = smrSystem.PerturbMessages(&eventmangler.ModuleParams{
			MinDelay: p.MinDelay,
			MaxDelay: p.MaxDelay,
			DropRate: p.DropRate,
		})
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to configure SMR mangler: %w", validatorID, err)
		}
	}

	if err := smrSystem.Start(); err != nil {
		return nil, fmt.Errorf("validator %v failed to start SMR system: %w", validatorID, err)
	}

	nodeCfg := mir.DefaultNodeConfig().WithLogger(logger)
	m.MirNode, err = mir.NewNode(t.NodeID(mirID), nodeCfg, smrSystem.Modules(), nil, m.interceptor)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create Mir node: %w", validatorID, err)
	}

	return &m, nil
}

// Start starts the manager.
func (m *Manager) Start(ctx context.Context) chan error {
	log.With("validator", m.MirID).Infof("Mir manager %s starting", m.MirID)

	errChan := make(chan error, 1)

	go func() {
		// Run Mir node until it stops.
		errChan <- m.MirNode.Run(ctx)
	}()

	return errChan
}

// Stop stops the manager and all its components.
func (m *Manager) Stop() {
	log.With("validator", m.MirID).Infof("Mir manager shutting down")
	defer log.With("validator", m.MirID).Info("Mir manager shut down")

	if m.stopped {
		log.With("validator", m.MirID).Warnf("Mir manager has already been stopped")
		return
	}
	m.stopped = true

	if m.interceptor != nil {
		if err := m.interceptor.Stop(); err != nil {
			log.With("validator", m.MirID).Errorf("Could not close interceptor: %s", err)
		}
		log.With("validator", m.MirID).Info("Interceptor closed")
	}

	m.Net.Stop()
	log.With("validator", m.MirID).Info("Network transport stopped")

	close(m.stopCh)
	m.MirNode.Stop()
}

// ID prints Manager ID.
func (m *Manager) ID() string {
	return m.ValidatorID.String()
}

func (m *Manager) initCheckpoint(params trantor.Params, height abi.ChainEpoch) (*checkpoint.StableCheckpoint, error) {
	return GetCheckpointByHeight(m.StateManager.ctx, m.ds, height, &params)
}

// GetSignedMessages extracts Filecoin signed messages from a Mir batch.
func (m *Manager) GetSignedMessages(mirMsgs []Message) (msgs []*types.SignedMessage) {
	log.With("validator", m.MirID).Infof("received a block with %d messages", len(msgs))
	for _, tx := range mirMsgs {

		input, err := parseTx(tx)
		if err != nil {
			log.With("validator", m.MirID).Error("unable to decode a message in Mir block:", err)
			continue
		}

		switch msg := input.(type) {
		case *types.SignedMessage:
			// batch being processed, remove from mpool
			found := m.Pool.DeleteRequest(msg.Cid(), msg.Message.Nonce)
			if !found {
				log.With("validator", m.MirID).
					Debugf("unable to find a message with %v hash in our local fifo.Pool", msg.Cid())
				// TODO: If we try to remove something from the pool, we should remember that
				// we already tried to remove that to avoid adding as it may lead to a dead-lock.
				// FIFO should be updated because we don't have the support for in-flight supports.
				// continue
			}
			msgs = append(msgs, msg)
			log.With("validator", m.MirID).Infof("got message: to=%s, nonce= %d", msg.Message.To, msg.Message.Nonce)
		default:
			log.With("validator", m.MirID).Error("unknown message type in a block")
		}
	}
	return
}

func (m *Manager) CreateTransportRequests(msgs []*types.SignedMessage) []*mirproto.Request {
	var requests []*mirproto.Request
	requests = append(requests, m.batchSignedMessages(msgs)...)
	return requests
}

// batchPushSignedMessages pushes signed messages into the request pool and sends them to Mir.
func (m *Manager) batchSignedMessages(msgs []*types.SignedMessage) (requests []*mirproto.Request) {
	for _, msg := range msgs {
		clientID := msg.Message.From.String()
		nonce := msg.Message.Nonce
		if !m.Pool.IsTargetRequest(clientID, nonce) {
			log.With("validator", m.MirID).Warnf("batchSignedMessage: target request not found for client ID")
			continue
		}

		data, err := MessageBytes(msg)
		if err != nil {
			log.With("validator", m.MirID).Errorf("error in message bytes in batchSignedMessage: %s", err)
			continue
		}

		r := &mirproto.Request{
			ClientId: clientID,
			ReqNo:    nonce,
			Type:     TransportRequest,
			Data:     data,
		}

		m.Pool.AddRequest(msg.Cid(), r)

		requests = append(requests, r)
	}
	return requests
}

func (m *Manager) CreateConfigurationRequest(set *validator.Set) *mirproto.Request {
	var b bytes.Buffer
	if err := set.MarshalCBOR(&b); err != nil {
		log.With("validator", m.MirID).Errorf("unable to marshall validator set: %v", err)
		return nil
	}

	r := mirproto.Request{
		ClientId: m.MirID,
		ReqNo:    m.reconfigurationNonce,
		Type:     ConfigurationRequest,
		Data:     b.Bytes(),
	}

	v, err := proto.Marshal(&r)
	if err != nil {
		log.With("validator", m.MirID).Errorf("unable to marshall configuration request: %v", err)
		return nil
	}

	if err := m.ds.Put(m.ctx, ConfigurationIndexKey(m.reconfigurationNonce), v); err != nil {
		log.With("validator", m.MirID).Errorf("unable to store configuration request: %v", err)
		panic(1)
		return nil
	}

	m.reconfigurationNonce++
	m.StoreSentConfigurationNumber(m.reconfigurationNonce)

	return &r
}

func (m *Manager) RecoverConfigurationData() ([]*mirproto.Request, error) {
	maxNonce := m.RecoverSentConfigurationNumber()
	minNonce := m.RecoverExecutedConfigurationNumber()

	m.reconfigurationNonce = maxNonce

	// Check do we need recovering configuration data or not.
	if maxNonce == minNonce && maxNonce == 0 {
		return nil, nil
	}

	if minNonce > maxNonce {
		return nil, fmt.Errorf("validator %v has incorrect configuration numbers: %d, %d", m.ValidatorID, minNonce, maxNonce)
	}

	var configRequests []*mirproto.Request

	for i := minNonce; i < maxNonce; i++ {
		b, err := m.ds.Get(m.ctx, ConfigurationIndexKey(i))
		if err != nil {
			return nil, err
		}

		r := mirproto.Request{}
		err = proto.Unmarshal(b, &r)
		if err != nil {
			log.With("validator", m.MirID).Errorf("unable to marshall configuration request: %v", err)
			return nil, err
		}

		configRequests = append(configRequests, &r)
	}

	return configRequests, nil
}

func (m *Manager) StoreSentConfigurationNumber(nonce uint64) {
	m.storeNumber(SentConfigurationNumberKey, nonce)
}

func (m *Manager) StoreExecutedConfigurationNumber(nonce uint64) {
	m.storeNumber(ExecutedConfigurationNumberKey, nonce)
}

func (m *Manager) RecoverSentConfigurationNumber() uint64 {
	return m.getNumber(SentConfigurationNumberKey)
}

func (m *Manager) RecoverExecutedConfigurationNumber() uint64 {
	return m.getNumber(ExecutedConfigurationNumberKey)
}

func (m *Manager) storeNumber(key datastore.Key, n uint64) {
	rb := make([]byte, 8)
	binary.LittleEndian.PutUint64(rb, n)
	if err := m.ds.Put(m.ctx, key, rb); err != nil {
		log.With("validator", m.MirID).Warnf("failed to put configuration number: %v", err)
	}
}

func (m *Manager) getNumber(key datastore.Key) uint64 {
	b, err := m.ds.Get(m.ctx, key)
	if err != nil {
		log.With("validator", m.MirID).Warnf("failed to get configuration number: %v", err)
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (m *Manager) RemoveAppliedConfigurationRequest(nonce uint64) {
	if err := m.ds.Delete(m.ctx, ConfigurationIndexKey(nonce)); err != nil {
		log.With("validator", m.MirID).Warnf("failed to remove applied configuration request %d: %v", nonce, err)
	}
}

func ConfigurationIndexKey(nonce uint64) datastore.Key {
	return datastore.NewKey(ConfigurationRequestsDBPrefix + strconv.FormatUint(nonce, 10))
}
