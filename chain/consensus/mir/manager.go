// Package mir implements ISS consensus protocol using the Mir protocol framework.
package mir

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

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
	InterceptorOutputEnv = "MIR_INTERCEPTOR_OUTPUT"
	ManglerEnv           = "MIR_MANGLER"

	CheckpointDBKeyPrefix = "mir/checkpoints/"

	ReconfigurationInterval = 2000 * time.Millisecond
)

type Manager struct {
	ctx context.Context
	ds  db.DB

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
	confManager   *ConfigurationManager
	stateManager  *StateManager
	interceptor   *eventlog.Recorder
	mirReady      chan chan []*mirproto.Request
	stopCh        chan struct{}
	membership    validator.Reader
	stopped       bool

	// Reconfiguration types.
	initialValidatorSet *validator.Set

	// Checkpoint types.
	segmentLength  int    // Segment length determining the checkpoint period.
	checkpointRepo string // Path where checkpoints are (optionally) persisted
}

func NewManager(ctx context.Context,
	addr address.Address,
	h host.Host,
	api v1api.FullNode,
	ds db.DB,
	membership validator.Reader,
	cfg *Config,
) (*Manager, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}
	mirID := addr.String()

	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	if cfg.SegmentLength < 0 {
		return nil, fmt.Errorf("validator %v segment length must not be negative", mirID)
	}

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
	if err := netTransport.Start(); err != nil {
		return nil, fmt.Errorf("failed to start transport: %w", err)
	}
	netTransport.Connect(initialMembership)

	cryptoManager, err := NewCryptoManager(addr, api)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create crypto manager: %w", addr, err)
	}

	confManager, err := NewConfigurationManager(ctx, ds, mirID)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create configuration manager: %w", addr, err)
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
		cryptoManager:       cryptoManager,
		confManager:         confManager,
		net:                 netTransport,
		ds:                  ds,
		initialValidatorSet: initialValidatorSet,
		mirReady:            make(chan chan []*mirproto.Request),
		checkpointRepo:      cfg.CheckpointRepo,
		segmentLength:       cfg.SegmentLength,
	}

	m.stateManager, err = NewStateManager(ctx, initialMembership, &m, m.confManager, api)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to start mir state manager: %w", mirID, err)
	}

	// Create SMR modules.
	mpool := pool.NewModule(
		m.mirReady,
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

	if interceptorPath := os.Getenv(InterceptorOutputEnv); interceptorPath != "" {
		// TODO: Persist in repo path?
		log.Infof("Interceptor initialized on %s", interceptorPath)
		m.interceptor, err = eventlog.NewRecorder(
			t.NodeID(mirID),
			path.Join(interceptorPath, cfg.GroupName, mirID),
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
	log.With("validator", m.mirID).Info("Mir manager serve starting")
	defer log.With("validator", m.mirID).Info("Mir manager serve stopped")

	log.With("validator", m.mirID).
		Infof("Mir info:\n\tNetwork - %v\n\tValidator ID - %v\n\tMir peerID - %v\n\tValidators - %v",
			m.netName, m.mirID, m.mirID, m.initialValidatorSet.GetValidators())

	mirErrChan := make(chan error, 1)
	go func() {
		// Run Mir node until it stops.
		mirErrChan <- m.mirNode.Run(ctx)
	}()
	// Perform cleanup of Node's modules and ensure that mir is closed when we stop mining.
	defer func() {
		m.Stop()
	}()

	reconfigure := time.NewTicker(ReconfigurationInterval)
	defer reconfigure.Stop()

	configRequests, err := m.confManager.Pending()
	if err != nil {
		return fmt.Errorf("validator %v failed to get pending confgiguration requests: %w", m.mirID, err)
	}

	lastValidatorSet := m.initialValidatorSet

	for {

		select {
		case <-ctx.Done():
			log.With("validator", m.mirID).Debug("Mir manager: context closed")
			return nil

		// First catch potential errors when mining.
		case err := <-mirErrChan:
			log.With("validator", m.mirID).Info("manager received error:", err)
			if err != nil && !errors.Is(err, mir.ErrStopped) {
				panic(fmt.Sprintf("validator %s consensus error: %v", m.mirID, err))
			}
			log.With("validator", m.mirID).Infof("Mir node stopped signal")
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
			r := m.createAndStoreConfigurationRequest(newSet)
			if r != nil {
				configRequests = append(configRequests, r)
			}

		case mirChan := <-m.mirReady:
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

			requests := m.createTransportRequests(msgs)

			if len(configRequests) > 0 {
				requests = append(requests, configRequests...)
			}

			mirChan <- requests
		}
	}
}

// Stop stops the manager and all its components.
func (m *Manager) Stop() {
	log.With("validator", m.mirID).Infof("Mir manager stopping")
	defer log.With("validator", m.mirID).Info("Mir manager stopped")

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

func (m *Manager) createTransportRequests(msgs []*types.SignedMessage) []*mirproto.Request {
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

func (m *Manager) createAndStoreConfigurationRequest(set *validator.Set) *mirproto.Request {
	var b bytes.Buffer
	if err := set.MarshalCBOR(&b); err != nil {
		log.With("validator", m.mirID).Errorf("unable to marshall validator set: %v", err)
		return nil
	}

	r, err := m.confManager.NewTX(ConfigurationRequest, b.Bytes())
	if err != nil {
		log.With("validator", m.mirID).Errorf("unable to create configuration tx: %v", err)
		return nil
	}

	return r
}
