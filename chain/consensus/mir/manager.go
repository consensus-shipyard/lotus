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
	id  string

	// Persistent storage.
	ds db.DB

	// Lotus types.
	netName   dtypes.NetworkName
	lotusNode v1api.FullNode

	// Mir types.
	mirNode         *mir.Node
	requestPool     *fifo.Pool
	wal             *simplewal.WAL
	net             net.Transport
	interceptor     *eventlog.Recorder
	readyForTxsChan chan chan []*mirproto.Request
	stopChan        chan struct{}
	stopped         bool
	cryptoManager   *CryptoManager
	confManager     *ConfigurationManager
	stateManager    *StateManager

	// Reconfiguration types.
	initialValidatorSet *validator.Set
	membership          validator.Reader
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
	id := addr.String()

	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	if cfg.SegmentLength < 0 {
		return nil, fmt.Errorf("validator %v segment length must not be negative", id)
	}

	initialValidatorSet, err := membership.GetValidatorSet()
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to get validator set: %w", id, err)
	}
	if initialValidatorSet.Size() == 0 {
		return nil, fmt.Errorf("validator %v: empty validator set", id)
	}

	_, initialMembership, err := validator.Membership(initialValidatorSet.Validators)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to build node membership: %w", id, err)
	}

	_, ok := initialMembership[t.NodeID(id)]
	if !ok {
		return nil, fmt.Errorf("validator %v failed to find its identity in membership", id)
	}

	logger := newManagerLogger(id)

	// Create Mir modules.
	netTransport := mirlibp2p.NewTransport(mirlibp2p.DefaultParams(), t.NodeID(id), h, logger)
	if err := netTransport.Start(); err != nil {
		return nil, fmt.Errorf("failed to start transport: %w", err)
	}
	netTransport.Connect(initialMembership)

	cryptoManager, err := NewCryptoManager(addr, api)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create crypto manager: %w", id, err)
	}

	confManager, err := NewConfigurationManager(ctx, ds, id)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create configuration manager: %w", id, err)
	}

	m := Manager{
		ctx:                 ctx,
		id:                  id,
		ds:                  ds,
		netName:             netName,
		lotusNode:           api,
		stopChan:            make(chan struct{}),
		readyForTxsChan:     make(chan chan []*mirproto.Request),
		requestPool:         fifo.New(),
		net:                 netTransport,
		cryptoManager:       cryptoManager,
		confManager:         confManager,
		initialValidatorSet: initialValidatorSet,
		membership:          membership,
	}

	m.stateManager, err = NewStateManager(ctx, addr, initialMembership, m.confManager, api, ds, m.requestPool, cfg)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to start mir state manager: %w", id, err)
	}

	// Create SMR modules.
	mpool := pool.NewModule(
		m.readyForTxsChan,
		pool.DefaultModuleConfig(),
		pool.DefaultModuleParams(),
	)

	params := trantor.DefaultParams(initialMembership)
	params.Iss.SegmentLength = cfg.SegmentLength // Segment length determining the checkpoint period.
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
			return nil, fmt.Errorf("validator %v failed to get initial snapshot SMR system: %w", id, err)
		}
	}

	smrSystem, err := trantor.New(
		t.NodeID(id),
		netTransport,
		initCh,
		m.cryptoManager,
		m.stateManager,
		params,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create SMR system: %w", id, err)
	}

	smrSystem = smrSystem.
		WithModule("mempool", mpool).
		WithModule("hasher", mircrypto.NewHasher(crypto.SHA256)) // to use sha256 hash from cryptomodule.

	mirManglerParams := os.Getenv(ManglerEnv)
	if mirManglerParams != "" {
		p, err := GetEnvManglerParams()
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to get mangler params: %w", id, err)
		}
		err = smrSystem.PerturbMessages(&eventmangler.ModuleParams{
			MinDelay: p.MinDelay,
			MaxDelay: p.MaxDelay,
			DropRate: p.DropRate,
		})
		if err != nil {
			return nil, fmt.Errorf("validator %v failed to configure SMR mangler: %w", id, err)
		}
	}

	if err := smrSystem.Start(); err != nil {
		return nil, fmt.Errorf("validator %v failed to start SMR system: %w", id, err)
	}

	nodeCfg := mir.DefaultNodeConfig().WithLogger(logger)

	if interceptorPath := os.Getenv(InterceptorOutputEnv); interceptorPath != "" {
		// TODO: Persist in repo path?
		log.Infof("Interceptor initialized on %s", interceptorPath)
		m.interceptor, err = eventlog.NewRecorder(
			t.NodeID(id),
			path.Join(interceptorPath, cfg.GroupName, id),
			logging.Decorate(logger, "Interceptor: "),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create interceptor: %w", err)
		}
		m.mirNode, err = mir.NewNode(t.NodeID(id), nodeCfg, smrSystem.Modules(), nil, m.interceptor)
	} else {
		m.mirNode, err = mir.NewNode(t.NodeID(id), nodeCfg, smrSystem.Modules(), nil, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create Mir node: %w", id, err)
	}

	return &m, nil
}

func (m *Manager) Serve(ctx context.Context) error {
	log.With("validator", m.id).Info("Mir manager serve starting")
	defer log.With("validator", m.id).Info("Mir manager serve stopped")

	log.With("validator", m.id).
		Infof("Mir info:\n\tNetwork - %v\n\tValidator ID - %v\n\tMir peerID - %v\n\tValidators - %v",
			m.netName, m.id, m.id, m.initialValidatorSet.GetValidators())

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
		return fmt.Errorf("validator %v failed to get pending confgiguration requests: %w", m.id, err)
	}

	lastValidatorSet := m.initialValidatorSet

	for {

		select {
		case <-ctx.Done():
			log.With("validator", m.id).Debug("Mir manager: context closed")
			return nil

		// First catch potential errors when mining.
		case err := <-mirErrChan:
			log.With("validator", m.id).Info("manager received error:", err)
			if err != nil && !errors.Is(err, mir.ErrStopped) {
				panic(fmt.Sprintf("validator %s consensus error: %v", m.id, err))
			}
			log.With("validator", m.id).Infof("Mir node stopped signal")
			return nil

		case <-reconfigure.C:
			// Send a reconfiguration transaction if the validator set in the actor has been changed.
			newSet, err := m.membership.GetValidatorSet()
			if err != nil {
				log.With("validator", m.id).Warnf("failed to get subnet validators: %v", err)
				continue
			}

			if lastValidatorSet.Equal(newSet) {
				continue
			}

			log.With("validator", m.id).
				Infof("new validator set: number: %d, size: %d, members: %v",
					newSet.ConfigurationNumber, newSet.Size(), newSet.GetValidatorIDs())

			lastValidatorSet = newSet
			r := m.createAndStoreConfigurationRequest(newSet)
			if r != nil {
				configRequests = append(configRequests, r)
			}

		case mirChan := <-m.readyForTxsChan:
			base, err := m.stateManager.api.ChainHead(ctx)
			if err != nil {
				return xerrors.Errorf("validator %v failed to get chain head: %w", m.id, err)
			}
			log.With("validator", m.id).Debugf("selecting messages from mempool from base: %v", base.Key())
			msgs, err := m.lotusNode.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.With("validator", m.id).With("epoch", base.Height()).
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
	log.With("validator", m.id).Infof("Mir manager stopping")
	defer log.With("validator", m.id).Info("Mir manager stopped")

	if m.stopped {
		log.With("validator", m.id).Warnf("Mir manager has already been stopped")
		return
	}
	m.stopped = true

	if m.interceptor != nil {
		if err := m.interceptor.Stop(); err != nil {
			log.With("validator", m.id).Errorf("Could not close interceptor: %s", err)
		}
		log.With("validator", m.id).Info("Interceptor closed")
	}

	m.net.Stop()
	log.With("validator", m.id).Info("Network transport stopped")

	close(m.stopChan)
	m.mirNode.Stop()
}

func (m *Manager) initCheckpoint(params trantor.Params, height abi.ChainEpoch) (*checkpoint.StableCheckpoint, error) {
	return GetCheckpointByHeight(m.stateManager.ctx, m.ds, height, &params)
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
		if !m.requestPool.IsTargetRequest(clientID, nonce) {
			log.With("validator", m.id).Warnf("batchSignedMessage: target request not found for client ID")
			continue
		}

		data, err := MessageBytes(msg)
		if err != nil {
			log.With("validator", m.id).Errorf("error in message bytes in batchSignedMessage: %s", err)
			continue
		}

		r := &mirproto.Request{
			ClientId: clientID,
			ReqNo:    nonce,
			Type:     TransportRequest,
			Data:     data,
		}

		m.requestPool.AddRequest(msg.Cid(), r)

		requests = append(requests, r)
	}
	return requests
}

func (m *Manager) createAndStoreConfigurationRequest(set *validator.Set) *mirproto.Request {
	var b bytes.Buffer
	if err := set.MarshalCBOR(&b); err != nil {
		log.With("validator", m.id).Errorf("unable to marshall validator set: %v", err)
		return nil
	}

	r, err := m.confManager.NewTX(ConfigurationRequest, b.Bytes())
	if err != nil {
		log.With("validator", m.id).Errorf("unable to create configuration tx: %v", err)
		return nil
	}

	return r
}
