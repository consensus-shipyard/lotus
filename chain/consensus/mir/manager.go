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

	"github.com/consensus-shipyard/go-ipc-types/validator"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	mircrypto "github.com/filecoin-project/mir/pkg/crypto"
	"github.com/filecoin-project/mir/pkg/eventlog"
	"github.com/filecoin-project/mir/pkg/eventmangler"
	"github.com/filecoin-project/mir/pkg/logging"
	"github.com/filecoin-project/mir/pkg/net"
	mirlibp2p "github.com/filecoin-project/mir/pkg/net"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/simplewal"
	"github.com/filecoin-project/mir/pkg/systems/trantor"
	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	mirmembership "github.com/filecoin-project/lotus/chain/consensus/mir/membership"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/fifo"
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
	mirCtx          context.Context
	mirErrChan      chan error
	mirCancel       context.CancelFunc
	mirNode         *mir.Node
	requestPool     *fifo.Pool
	wal             *simplewal.WAL
	net             net.Transport
	interceptor     *eventlog.Recorder
	readyForTxsChan chan chan []*mirproto.Request
	stopped         bool
	cryptoManager   *CryptoManager
	confManager     *ConfigurationManager
	stateManager    *StateManager

	// Reconfiguration types.
	initialValidatorSet *validator.Set
	membership          mirmembership.Reader
}

func NewManager(ctx context.Context,
	net mirlibp2p.Transport,
	node v1api.FullNode,
	ds db.DB,
	membership mirmembership.Reader,
	cfg *Config,
) (*Manager, error) {
	netName, err := node.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}
	id := cfg.Addr.String()

	if cfg.Consensus.SegmentLength < 0 {
		return nil, fmt.Errorf("validator %v segment length must not be negative", id)
	}

	membershipInfo, err := membership.GetMembershipInfo()
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to get membership info: %w", id, err)
	}

	initialValidatorSet := membershipInfo.ValidatorSet
	genesisEpoch := membershipInfo.GenesisEpoch
	valSize := initialValidatorSet.Size()
	// There needs to be at least one validator in the membership
	if valSize == 0 {
		return nil, fmt.Errorf("validator %v: empty validator set", id)
	}
	// Check the minimum number of validators.
	if membershipInfo.MinValidators > uint64(valSize) {
		return nil, fmt.Errorf("validator %v: minimum number of validators not reached", id)
	}

	_, initialMembership, err := mirmembership.Membership(initialValidatorSet.Validators)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to build node membership: %w", id, err)
	}

	_, ok := initialMembership[t.NodeID(id)]
	if !ok {
		return nil, fmt.Errorf("validator %v failed to find its identity in membership", id)
	}

	logger := NewLogger(id)

	// Create Mir modules.
	if err := net.Start(); err != nil {
		return nil, fmt.Errorf("failed to start transport: %w", err)
	}
	net.Connect(initialMembership)

	cryptoManager, err := NewCryptoManager(cfg.Addr, node)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create crypto manager: %w", id, err)
	}

	confManager, err := NewConfigurationManagerWithMembershipInfo(ctx, ds, id, membershipInfo)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create configuration manager: %w", id, err)
	}

	m := Manager{
		ctx:                 ctx,
		id:                  id,
		ds:                  ds,
		netName:             netName,
		lotusNode:           node,
		readyForTxsChan:     make(chan chan []*mirproto.Request),
		requestPool:         fifo.New(),
		cryptoManager:       cryptoManager,
		confManager:         confManager,
		net:                 net,
		initialValidatorSet: initialValidatorSet,
		membership:          membership,
	}
	m.mirErrChan = make(chan error, 1)
	m.mirCtx, m.mirCancel = context.WithCancel(context.Background())

	m.stateManager, err = NewStateManager(ctx, m.netName, initialMembership, abi.ChainEpoch(genesisEpoch), m.confManager, node, ds, m.requestPool, cfg)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to start mir state manager: %w", id, err)
	}

	params := trantor.DefaultParams(initialMembership)
	params.Iss.SegmentLength = cfg.Consensus.SegmentLength // Segment length determining the checkpoint period.
	params.Iss.ConfigOffset = cfg.Consensus.ConfigOffset
	params.Iss.AdjustSpeed(cfg.Consensus.MaxProposeDelay)
	params.Iss.PBFTViewChangeSNTimeout = cfg.Consensus.PBFTViewChangeSNTimeout
	params.Iss.PBFTViewChangeSegmentTimeout = cfg.Consensus.PBFTViewChangeSegmentTimeout
	params.Mempool.MaxTransactionsInBatch = cfg.Consensus.MaxTransactionsInBatch
	params.Mempool.TxFetcher = pool.NewFetcher(ctx, m.readyForTxsChan).Fetch

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
		net,
		initCh,
		m.cryptoManager,
		m.stateManager,
		params,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("validator %v failed to create SMR system: %w", id, err)
	}

	smrSystem = smrSystem.WithModule("hasher", mircrypto.NewHasher(crypto.SHA256)) // to use sha256 hash from cryptomodule.

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
	log.With("validator", m.id).Info("Mir manager serve started")
	defer log.With("validator", m.id).Info("Mir manager serve stopped")

	log.With("validator", m.id).
		Infof("Mir info:\n\tNetwork - %v\n\tValidator ID - %v\n\tMir peerID - %v\n\tValidators - %v",
			m.netName, m.id, m.id, m.initialValidatorSet.GetValidators())

	go func() {
		// Run Mir node until it stops.
		// We pass a new cancellable context to Run() to be sure that if the Lotus context is closed then the Mir
		// node will not be stopped implicitly and there will be no race between Lotus and Mir during shutdown process.
		// In this case we also know that if we receive an error on mirErrChan before cancelling mirCtx
		// then that error is not ErrStopped.
		m.mirErrChan <- m.mirNode.Run(m.mirCtx)
	}()
	defer m.stop()

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
			log.With("validator", m.id).Info("Mir manager: context closed")
			return nil

		case err := <-m.mirErrChan:
			panic(fmt.Sprintf("Mir node %v running error: %v", m.id, err))

		case <-reconfigure.C:
			// Send a reconfiguration transaction if the validator set in the actor has been changed.
			mInfo, err := m.membership.GetMembershipInfo()
			if err != nil {
				log.With("validator", m.id).Warnf("failed to get subnet validators: %v", err)
				continue
			}
			newSet := mInfo.ValidatorSet
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
			if ctx.Err() != nil {
				log.With("validator", m.id).Info("Mir manager: context closed before calling ChainHead")
				return nil
			}
			base, err := m.lotusNode.ChainHead(ctx)
			if err != nil {
				return xerrors.Errorf("validator %v failed to get chain head: %w", m.id, err)
			}
			log.With("validator", m.id).Debugf("selecting messages from mempool for base: %v", base.Key())
			msgs, err := m.lotusNode.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.With("validator", m.id).With("epoch", base.Height()).
					Errorw("failed to select messages from mempool", "error", err)
			}

			requests := m.createTransportRequests(msgs)

			if len(configRequests) > 0 {
				requests = append(requests, configRequests...)
			}

			select {
			case <-ctx.Done():
				log.With("validator", m.id).Info("Mir manager: context closed while sending txs")
				return nil
			case mirChan <- requests:
			}
		}
	}
}

// stop stops the manager and all its components.
func (m *Manager) stop() {
	log.With("validator", m.id).Infof("Mir manager stop() started")
	defer log.With("validator", m.id).Info("Mir manager stop() finished")

	if m.stopped {
		log.With("validator", m.id).Warnf("Mir manager has already been stopped")
		return
	}
	m.stopped = true

	// Cancel Mir Context.
	m.mirCancel()

	// Stop components used by the Mir node.

	if m.interceptor != nil {
		if err := m.interceptor.Stop(); err != nil {
			log.With("validator", m.id).Errorf("Could not stop interceptor: %s", err)
		} else {
			log.With("validator", m.id).Info("Interceptor stopped")
		}
	}

	m.net.Stop()
	log.With("validator", m.id).Info("Network transport stopped")

	m.mirNode.Stop()
	err := <-m.mirErrChan
	if !errors.Is(err, mir.ErrStopped) {
		log.With("validator", m.id).Errorf("Mir node stopped with error: %v", err)
	} else {
		log.With("validator", m.id).Infof("Mir node stopped")
	}
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
