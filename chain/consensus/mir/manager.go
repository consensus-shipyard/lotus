// Package mir implements ISS consensus protocol using the Mir protocol framework.
package mir

import (
	"bytes"
	"context"
	"crypto"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p-core/host"
	"golang.org/x/xerrors"

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

	ReconfigurationInterval = 2000 * time.Millisecond
)

var (
	LatestCheckpointKey   = datastore.NewKey("mir/latest-check")
	LatestCheckpointPbKey = datastore.NewKey("mir/latest-check-pb")

	// SentConfigurationNumberKey is used to store SentConfigurationNumber
	// that is the maximum configuration request number that have been sent.
	SentConfigurationNumberKey = datastore.NewKey("mir/sent-config-number")
	// AppliedConfigurationNumberKey is used to store AppliedConfigurationNumber
	// that is the maximum configuration request number that have been applied.
	AppliedConfigurationNumberKey = datastore.NewKey("mir/applied-config-number")
	// ReconfigurationVotesKey is used to store reconfiguration votes.
	ReconfigurationVotesKey = datastore.NewKey("mir/reconfiguration-votes")
)

// Manager manages the Lotus and Mir nodes participating in ISS consensus protocol.
type Manager struct {
	ctx context.Context
	// Lotus types.
	netName   dtypes.NetworkName
	Pool      *fifo.Pool
	lotusNode v1api.FullNode
	lotusID   address.Address

	// Mir types.
	mirNode       *mir.Node
	mirID         string
	wal           *simplewal.WAL
	net           net.Transport
	cryptoManager *CryptoManager
	stateManager  *StateManager
	interceptor   *eventlog.Recorder
	toMir         chan chan []*mirproto.Request
	ds            db.DB
	stopCh        chan struct{}
	stopped       bool

	// Reconfiguration types.
	initialValidatorSet  *validator.Set
	reconfigurationNonce uint64
	membership           validator.Reader

	// Checkpoints
	segmentLength  int    // segment length determining the checkpoint period.
	checkpointRepo string // path where checkpoints are (optionally) persisted
}

func NewManager(ctx context.Context, addr address.Address, h host.Host, api v1api.FullNode, ds db.DB,
	membership validator.Reader, cfg *Config) (*Manager, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}
	mirID := addr.String()

	initialValidatorSet, err := membership.GetValidatorSet()
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to get validator set: %w", mirID, err)
	}
	if initialValidatorSet.Size() == 0 {
		return nil, fmt.Errorf("validator %v: empty validator set", mirID)
	}

	_, initialMembership, err := validator.Membership(initialValidatorSet.Validators)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to build node membership: %w", mirID, err)
	}

	_, ok := initialMembership[t.NodeID(mirID)]
	if !ok {
		return nil, fmt.Errorf("validator %v failed to find its identity in membership", mirID)
	}

	logger := newManagerLogger(mirID)

	// Create Mir modules.
	netTransport := mirlibp2p.NewTransport(mirlibp2p.DefaultParams(), t.NodeID(mirID), h, logger)

	cryptoManager, err := NewCryptoManager(addr, api)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create crypto manager: %w", addr, err)
	}

	var interceptor *eventlog.Recorder

	if cfg.SegmentLength < 0 {
		return nil, fmt.Errorf("validator %v segment length must not be negative", mirID)
	}

	m := Manager{
		ctx:                 ctx,
		stopCh:              make(chan struct{}),
		lotusID:             addr,
		lotusNode:           api,
		membership:          membership,
		netName:             netName,
		Pool:                fifo.New(),
		mirID:               mirID,
		interceptor:         interceptor,
		cryptoManager:       cryptoManager,
		net:                 netTransport,
		ds:                  ds,
		initialValidatorSet: initialValidatorSet,
		toMir:               make(chan chan []*mirproto.Request),
		checkpointRepo:      cfg.CheckpointRepo,
		segmentLength:       cfg.SegmentLength,
	}

	m.stateManager, err = NewStateManager(ctx, initialMembership, &m, api)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to start mir state manager: %w", mirID, err)
	}

	// Create SMR modules.
	mpool := pool.NewModule(
		m.toMir,
		pool.DefaultModuleConfig(),
		pool.DefaultModuleParams(),
	)

	params := trantor.DefaultParams(initialMembership)
	params.Iss.SegmentLength = m.segmentLength
	params.Mempool.MaxTransactionsInBatch = 1024
	params.Iss.AdjustSpeed(1 * time.Second)
	params.Iss.ConfigOffset = ConfigOffset
	params.Iss.PBFTViewChangeSNTimeout = 6 * time.Second
	params.Iss.PBFTViewChangeSegmentTimeout = 6 * time.Second

	initCh := cfg.InitialCheckpoint
	// if no initial checkpoint provided in config
	if initCh == nil {
		initCh, err = m.initCheckpoint(params, 0)
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to get initial snapshot SMR system: %w", mirID, err)
		}
	}

	smrSystem, err := trantor.New(
		t.NodeID(mirID),
		netTransport,
		initCh,
		m.cryptoManager,
		m.stateManager,
		params,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create SMR system: %w", mirID, err)
	}

	smrSystem = smrSystem.
		WithModule("mempool", mpool).
		WithModule("hasher", mircrypto.NewHasher(crypto.SHA256)) // to use sha256 hash from cryptomodule.

	mirManglerParams := os.Getenv(ManglerEnv)
	if mirManglerParams != "" {
		p, err := GetEnvManglerParams()
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to get mangler params: %w", mirID, err)
		}
		err = smrSystem.PerturbMessages(&eventmangler.ModuleParams{
			MinDelay: p.MinDelay,
			MaxDelay: p.MaxDelay,
			DropRate: p.DropRate,
		})
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to configure SMR mangler: %w", mirID, err)
		}
	}

	if err := smrSystem.Start(); err != nil {
		return nil, fmt.Errorf("validator %v failed to start SMR system: %w", mirID, err)
	}

	nodeCfg := mir.DefaultNodeConfig().WithLogger(logger)

	interceptorOutput := os.Getenv(InterceptorOutputEnv)
	if interceptorOutput != "" {
		// TODO: Persist in repo path?
		log.Infof("Interceptor initialized")
		m.interceptor, err = eventlog.NewRecorder(
			t.NodeID(mirID),
			interceptorOutput+"/"+mirID,
			logging.Decorate(logger, "Interceptor: "),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create interceptor: %w", err)
		}
		m.mirNode, err = mir.NewNode(t.NodeID(mirID), nodeCfg, smrSystem.Modules(), nil, m.interceptor)
	} else {
		m.mirNode, err = mir.NewNode(t.NodeID(mirID), nodeCfg, smrSystem.Modules(), nil, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create Mir node: %w", mirID, err)
	}

	return &m, nil
}

func (m *Manager) Serve(ctx context.Context) error {
	log.With("validator", m.mirID).Infof("Mir manager %s starting", m.mirID)
	defer log.With("validator", m.mirID).Infof("Mir manager %s stopped", m.mirID)

	log.With("validator", m.mirID).
		Infof("Mir info:\n\tNetwork - %v\n\tValidator ID - %v\n\tMir peerID - %v\n\tValidators - %v",
			m.netName, m.mirID, m.mirID, m.initialValidatorSet.GetValidators())

	errChan := make(chan error, 1)
	go func() {
		// Run Mir node until it stops.
		errChan <- m.mirNode.Run(ctx)
	}()
	// Perform cleanup of Node's modules and ensure that mir is closed when we stop mining.
	defer func() {
		m.Stop()
	}()

	reconfigure := time.NewTicker(ReconfigurationInterval)
	defer reconfigure.Stop()

	configRequests, err := m.RecoverConfigurationData()
	if err != nil {
		return fmt.Errorf("validator %v failed to recover confgiguration requests: %w", m.mirID, err)
	}

	lastValidatorSet := m.initialValidatorSet

	for {
		// Here we use `ctx.Err()` in the beginning of the `for` loop instead of using it in the `select` statement,
		// because if `ctx` has been closed then `api.ChainHead(ctx)` returns an error,
		// and we will be in the infinite loop due to `continue`.
		if ctx.Err() != nil {
			log.With("validator", m).Debug("Mir manager: context closed")
			return nil
		}

		select {

		// first catch potential errors when mining
		case err := <-errChan:
			log.With("validator", m.mirID).Info("manager received error:", err)
			if err != nil && !errors.Is(err, mir.ErrStopped) {
				panic(fmt.Sprintf("validator %s consensus error: %v", m.mirID, err))
			}
			log.With("validator", m.mirID).Infof("Mir node stopped signal")
			return nil

		case <-ctx.Done():
			log.With("validator", m.mirID).Debug("Mir manager: context closed")
			return nil

		case <-reconfigure.C:
			// Send a reconfiguration transaction if the validator set in the actor has been changed.
			newSet, err := m.membership.GetValidatorSet()
			if err != nil {
				log.With("validator", m.mirID).Warnf("failed to get subnet validators: %w", err)
				continue
			}

			if lastValidatorSet.Equal(newSet) {
				continue
			}

			log.With("validator", m.mirID).
				Infof("new validator set: number: %d, size: %d, members: %v",
					newSet.ConfigurationNumber, newSet.Size(), newSet.GetValidatorIDs())

			lastValidatorSet = newSet
			r := m.CreateConfigurationRequest(newSet)
			if r != nil {
				configRequests = append(configRequests, r)

			}

		case mirChan := <-m.toMir:
			base, err := m.stateManager.api.ChainHead(ctx)
			if err != nil {
				return xerrors.Errorf("validator %v failed to get chain head: %w", m.mirID, err)
			}
			log.With("validator", m.mirID).Debugf("selecting messages from mempool from base: %v", base.Key())
			msgs, err := m.lotusNode.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.With("validator", m.mirID).With("epoch", base.Height()).
					Errorw("failed to select messages from mempool", "error", err)
			}

			requests := m.CreateTransportRequests(msgs)

			if len(configRequests) > 0 {
				requests = append(requests, configRequests...)
			}

			mirChan <- requests
		}
	}
}

// Stop stops the manager and all its components.
func (m *Manager) Stop() {
	log.With("validator", m.mirID).Infof("Mir manager shutting down")
	defer log.With("validator", m.mirID).Info("Mir manager shut down")

	if m.stopped {
		log.With("validator", m.mirID).Warnf("Mir manager has already been stopped")
		return
	}
	m.stopped = true

	if m.interceptor != nil {
		if err := m.interceptor.Stop(); err != nil {
			log.With("validator", m.mirID).Errorf("Could not close interceptor: %s", err)
		}
		log.With("validator", m.mirID).Info("Interceptor closed")
	}

	m.net.Stop()
	log.With("validator", m.mirID).Info("Network transport stopped")

	close(m.stopCh)
	m.mirNode.Stop()
}

func (m *Manager) initCheckpoint(params trantor.Params, height abi.ChainEpoch) (*checkpoint.StableCheckpoint, error) {
	return GetCheckpointByHeight(m.stateManager.ctx, m.ds, height, &params)
}

// GetSignedMessages extracts Filecoin signed messages from a Mir batch.
func (m *Manager) GetSignedMessages(mirMsgs []Message) (msgs []*types.SignedMessage) {
	log.With("validator", m.mirID).Infof("received a block with %d messages", len(msgs))
	for _, tx := range mirMsgs {

		input, err := parseTx(tx)
		if err != nil {
			log.With("validator", m.mirID).Error("unable to decode a message in Mir block:", err)
			continue
		}

		switch msg := input.(type) {
		case *types.SignedMessage:
			// batch being processed, remove from mpool
			found := m.Pool.DeleteRequest(msg.Cid(), msg.Message.Nonce)
			if !found {
				log.With("validator", m.mirID).
					Debugf("unable to find a message with %v hash in our local fifo.Pool", msg.Cid())
				// TODO: If we try to remove something from the pool, we should remember that
				// we already tried to remove that to avoid adding as it may lead to a dead-lock.
				// FIFO should be updated because we don't have the support for in-flight supports.
				// continue
			}
			msgs = append(msgs, msg)
			log.With("validator", m.mirID).Infof("got message: to=%s, nonce= %d", msg.Message.To, msg.Message.Nonce)
		default:
			log.With("validator", m.mirID).Error("unknown message type in a block")
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
			log.With("validator", m.mirID).Warnf("batchSignedMessage: target request not found for client ID")
			continue
		}

		data, err := MessageBytes(msg)
		if err != nil {
			log.With("validator", m.mirID).Errorf("error in message bytes in batchSignedMessage: %s", err)
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
		log.With("validator", m.mirID).Errorf("unable to marshall validator set: %v", err)
		return nil
	}

	r := mirproto.Request{
		ClientId: m.mirID,
		ReqNo:    m.reconfigurationNonce,
		Type:     ConfigurationRequest,
		Data:     b.Bytes(),
	}

	v, err := proto.Marshal(&r)
	if err != nil {
		log.With("validator", m.mirID).Errorf("unable to marshall configuration request: %v", err)
		return nil
	}

	if err := m.ds.Put(m.ctx, ConfigurationIndexKey(m.reconfigurationNonce), v); err != nil {
		log.With("validator", m.mirID).Errorf("unable to store configuration request: %v", err)
		return nil
	}

	m.reconfigurationNonce++
	m.StoreSentConfigurationNumber(m.reconfigurationNonce)

	return &r
}

func (m *Manager) RecoverConfigurationData() ([]*mirproto.Request, error) {
	maxNonce := m.RecoverSentConfigurationNumber()
	minNonce := m.RecoverAppliedConfigurationNumber()

	m.reconfigurationNonce = maxNonce

	// Check do we need recovering configuration data or not.
	if maxNonce == minNonce && maxNonce == 0 {
		return nil, nil
	}

	if minNonce > maxNonce {
		return nil, fmt.Errorf("validator %v has incorrect configuration numbers: %d, %d", m.mirID, minNonce, maxNonce)
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
			log.With("validator", m.mirID).Errorf("unable to marshall configuration request: %v", err)
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
	m.storeNumber(AppliedConfigurationNumberKey, nonce)
}

func (m *Manager) RecoverSentConfigurationNumber() uint64 {
	b, err := m.ds.Get(m.ctx, SentConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", m.mirID).Info("stored sent configuration number not found")
		return 0
	}
	if err != nil {
		log.With("validator", m.mirID).Warnf("failed to get sent configuration number: %v", err)
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (m *Manager) RecoverAppliedConfigurationNumber() uint64 {
	b, err := m.ds.Get(m.ctx, AppliedConfigurationNumberKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", m.mirID).Info("stored executed configuration number not found")
		return 0
	}
	if err != nil {
		log.With("validator", m.mirID).Warnf("failed to get applied configuration number: %v", err)
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (m *Manager) RecoverReconfigurationVotes() map[uint64]map[string][]t.NodeID {
	votes := make(map[uint64]map[string][]t.NodeID)
	b, err := m.ds.Get(m.ctx, ReconfigurationVotesKey)
	if errors.Is(err, datastore.ErrNotFound) {
		log.With("validator", m.mirID).Info("stored reconfiguration votes not found")
		return votes
	}
	if err != nil {
		log.With("validator", m.mirID).Warnf("failed to get reconfiguration votes: %v", err)
		return votes
	}

	var r VoteRecords
	if err := r.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		log.With("validator", m.mirID).Warnf("failed to unmarshal reconfiguration votes: %v", err)
		return votes
	}
	votes = RestoreConfigurationVotes(r.Records)

	return votes
}

func (m *Manager) StoreReconfigurationVotes(votes map[uint64]map[string][]t.NodeID) error {
	recs := StoreConfigurationVotes(votes)
	r := VoteRecords{
		Records: recs,
	}

	b := new(bytes.Buffer)
	if err := r.MarshalCBOR(b); err != nil {
		return err
	}
	if err := m.ds.Put(m.ctx, ReconfigurationVotesKey, b.Bytes()); err != nil {
		log.With("validator", m.mirID).Warnf("failed to put reconfiguration votes: %v", err)
	}

	return nil
}

func (m *Manager) storeNumber(key datastore.Key, n uint64) {
	rb := make([]byte, 8)
	binary.LittleEndian.PutUint64(rb, n)
	if err := m.ds.Put(m.ctx, key, rb); err != nil {
		log.With("validator", m.mirID).Warnf("failed to put configuration number by %s: %v", key, err)
	}
}

func (m *Manager) RemoveAppliedConfigurationRequest(nonce uint64) {
	if err := m.ds.Delete(m.ctx, ConfigurationIndexKey(nonce)); err != nil {
		log.With("validator", m.mirID).Warnf("failed to remove applied configuration request %d: %v", nonce, err)
	}
}

func ConfigurationIndexKey(nonce uint64) datastore.Key {
	return datastore.NewKey(ConfigurationRequestsDBPrefix + strconv.FormatUint(nonce, 10))
}

func RestoreConfigurationVotes(voteRecords []VoteRecord) map[uint64]map[string][]t.NodeID {
	m := make(map[uint64]map[string][]t.NodeID)
	for _, v := range voteRecords {
		if _, exist := m[v.ConfigurationNumber]; !exist {
			m[v.ConfigurationNumber] = make(map[string][]t.NodeID)
		}
		for _, id := range v.VotedValidators {
			m[v.ConfigurationNumber][v.ValSetHash] = append(m[v.ConfigurationNumber][v.ValSetHash], id.NodeID())
		}
	}
	return m
}

func StoreConfigurationVotes(reconfigurationVotes map[uint64]map[string][]t.NodeID) (votesRecords []VoteRecord) {
	for n, hashToValidatorsVotes := range reconfigurationVotes {
		for h, nodeIDs := range hashToValidatorsVotes {
			e := VoteRecord{
				ConfigurationNumber: n,
				ValSetHash:          h,
				VotedValidators:     NewVotedValidators(nodeIDs...),
			}
			votesRecords = append(votesRecords, e)
		}
	}
	return
}
