// Package mir implements ISS consensus protocol using the Mir protocol framework.
package mir

import (
	"bytes"
	"context"
	"crypto"
	"fmt"
	"os"

	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p-core/host"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	mircrypto "github.com/filecoin-project/mir/pkg/crypto"
	"github.com/filecoin-project/mir/pkg/eventlog"
	"github.com/filecoin-project/mir/pkg/logging"
	"github.com/filecoin-project/mir/pkg/net"
	mirlibp2p "github.com/filecoin-project/mir/pkg/net/libp2p"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/simplewal"
	"github.com/filecoin-project/mir/pkg/systems/smr"
	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/fifo"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const (
	InterceptorOutputEnv = "MIR_INTERCEPTOR_OUTPUT"
)

var (
	LatestCheckpointKey   = datastore.NewKey("mir/latest-check")
	LatestCheckpointPbKey = datastore.NewKey("mir/latest-check-pb")
)

// Manager manages the Lotus and Mir nodes participating in ISS consensus protocol.
type Manager struct {
	ctx context.Context

	// Lotus related types.
	NetName dtypes.NetworkName
	Addr    address.Address
	Pool    *fifo.Pool

	// Mir related types.
	MirNode       *mir.Node
	MirID         string
	WAL           *simplewal.WAL
	Net           net.Transport
	CryptoManager *CryptoManager
	StateManager  *StateManager
	interceptor   *eventlog.Recorder
	ToMir         chan chan []*mirproto.Request
	segmentLength int // segment length determining the checkpoint period.
	ds            db.DB

	// Reconfiguration related types.
	InitialValidatorSet  *ValidatorSet
	reconfigurationNonce uint64
}

func NewManager(ctx context.Context, addr address.Address, h host.Host, api v1api.FullNode, ds db.DB, cfg *Config) (*Manager, error) {
	netName, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}

	initialValidatorSet, err := GetValidators(cfg.MembershipCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator set: %w", err)
	}
	if initialValidatorSet.Size() == 0 {
		return nil, fmt.Errorf("empty validator set")
	}

	nodeIDs, initialMembership, err := validatorsMembership(initialValidatorSet.Validators)
	if err != nil {
		return nil, fmt.Errorf("failed to build node membership: %w", err)
	}

	// Create (ConfigOffset + 1) copies of the initial membership,
	// since ConfigOffset determines the number of epochs after the current epoch
	// for which the membership configuration is fixed.
	// That is, if the current epoch is e,
	// the following ConfigOffset configurations are already fixed
	// and configuration submitted to Mir will be for e + ConfigOffset + 1.
	// This is why the first ConfigOffset + 1 epochs have the same initial configuration.
	// NOTE: The notion of an epoch here is NOT the same as in Filecoin consensus,
	// but describes a whole sequence of output blocks.
	memberships := make([]map[t.NodeID]t.NodeAddress, ConfigOffset+1)
	for i := 0; i < ConfigOffset+1; i++ {
		memberships[t.EpochNr(i)] = initialMembership
	}

	mirID := addr.String()
	mirAddr, ok := initialMembership[t.NodeID(mirID)]
	if !ok {
		return nil, fmt.Errorf("self identity not included in validator set")
	}

	log.Info("Lotus node's Mir ID: ", mirID)
	log.Info("Lotus node's address in Mir: ", mirAddr)
	log.Info("Mir nodes IDs: ", nodeIDs)
	log.Info("Mir node libp2p peerID: ", h.ID())
	log.Info("Mir nodes addresses: ", initialMembership)

	logger := newManagerLogger()

	// Create Mir modules.
	netTransport, err := mirlibp2p.NewTransport(mirlibp2p.DefaultParams(), h, t.NodeID(mirID), logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}
	if err := netTransport.Start(); err != nil {
		return nil, fmt.Errorf("failed to start transport: %w", err)
	}
	netTransport.Connect(initialMembership)

	cryptoManager, err := NewCryptoManager(addr, api)
	if err != nil {
		return nil, fmt.Errorf("failed to create crypto manager: %w", err)
	}

	// Instantiate an interceptor.
	var interceptor *eventlog.Recorder

	m := Manager{
		ctx:                 ctx,
		Addr:                addr,
		NetName:             netName,
		Pool:                fifo.New(),
		MirID:               mirID,
		interceptor:         interceptor,
		CryptoManager:       cryptoManager,
		Net:                 netTransport,
		ds:                  ds,
		InitialValidatorSet: initialValidatorSet,
		ToMir:               make(chan chan []*mirproto.Request),
	}

	m.StateManager, err = NewStateManager(initialMembership, &m, api)
	if err != nil {
		return nil, fmt.Errorf("error starting mir state manager: %w", err)
	}

	mpool := pool.NewModule(
		m.ToMir,
		pool.DefaultModuleConfig(),
		pool.DefaultModuleParams(),
	)

	params := smr.DefaultParams(initialMembership)
	// configure SegmentLength for specific checkpoint period.
	m.segmentLength, err = segmentForCheckpointPeriod(cfg.CheckpointPeriod, initialMembership)
	if err != nil {
		return nil, fmt.Errorf("error getting segment length: %w", err)
	}
	params.Iss.SegmentLength = m.segmentLength

	latestCh, err := m.latestCheckpoint(params)
	if err != nil {
		return nil, fmt.Errorf("error getting inital snapshot SMR system: %w", err)
	}

	smrSystem, err := smr.New(
		t.NodeID(mirID),
		h,
		latestCh,
		m.CryptoManager,
		m.StateManager,
		params,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create SMR system: %w", err)
	}
	smrSystem = smrSystem.
		WithModule("mempool", mpool).
		WithModule("hasher", mircrypto.NewHasher(crypto.SHA256)) // to use sha256 hash from cryptomodule.

	if err := smrSystem.Start(); err != nil {
		return nil, fmt.Errorf("could not start SMR system: %w", err)
	}

	nodeCfg := mir.DefaultNodeConfig().WithLogger(logger)

	interceptorOutput := os.Getenv(InterceptorOutputEnv)
	if interceptorOutput != "" {
		// TODO: Persist in repo path?
		m.interceptor, err = eventlog.NewRecorder(
			t.NodeID(mirID),
			interceptorOutput,
			logging.Decorate(logger, "Interceptor: "),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create interceptor: %w", err)
		}
		m.MirNode, err = mir.NewNode(t.NodeID(mirID), nodeCfg, smrSystem.Modules(), nil, m.interceptor)
	} else {
		m.MirNode, err = mir.NewNode(t.NodeID(mirID), nodeCfg, smrSystem.Modules(), nil, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create Mir node: %w", err)
	}

	return &m, nil
}

// Start starts the manager.
func (m *Manager) Start(ctx context.Context) chan error {
	log.Infof("Mir manager %s starting", m.MirID)
	log.Info("Mir initial checkpointing period: ", m.GetCheckpointPeriod())

	errChan := make(chan error, 1)

	go func() {
		// Run Mir node until it stops.
		errChan <- m.MirNode.Run(ctx)
	}()

	return errChan
}

// Stop stops the manager and all its components.
func (m *Manager) Stop() {
	log.With("miner", m.MirID).Infof("Mir manager shutting down")
	defer log.With("miner", m.MirID).Info("Mir manager stopped")

	if m.interceptor != nil {
		if err := m.interceptor.Stop(); err != nil {
			log.With("miner", m.MirID).Errorf("Could not close interceptor: %s", err)
		}
		log.With("miner", m.MirID).Info("Interceptor closed")
	}

	m.Net.Stop()
	log.With("miner", m.MirID).Info("Network transport stopped")

	m.MirNode.Stop()
}

// ReconfigureMirNode reconfigures the Mir node.
func (m *Manager) ReconfigureMirNode(ctx context.Context, nodes map[t.NodeID]t.NodeAddress) error {
	log.With("miner", m.MirID).Debug("Reconfiguring a Mir node")

	if len(nodes) == 0 {
		return fmt.Errorf("empty validator set")
	}

	go m.Net.Connect(nodes)
	// Per comment https://github.com/consensus-shipyard/lotus/pull/14#discussion_r993162569,
	// CloseOldConnections should only be used after a stable checkpoint when a reconfiguration is applied
	// (as there is where we have the config information). This functions should be called
	// in the garbage collection process performed when the reconfiguration is effective.
	// go m.Net.CloseOldConnections(nodes)

	return nil
}

func parseTx(tx []byte) (interface{}, error) {
	ln := len(tx)
	// This is very simple input validation to be protected against invalid messages.
	// TODO: Make this smarter.
	if ln <= 2 {
		return nil, fmt.Errorf("mir tx len %d is too small", ln)
	}

	var err error
	var msg interface{}

	// TODO: Consider taking it out to a MirMessageFromBytes function
	// into mir/types.go so that we have all msgType functionality in
	// the same place.
	lastByte := tx[ln-1]
	switch lastByte {
	case SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1])
	case ConfigMessageType:
		return nil, fmt.Errorf("config message is not supported")
	default:
		err = fmt.Errorf("unknown message type %d", lastByte)
	}

	if err != nil {
		return nil, err
	}

	return msg, nil
}

// GetMessages extracts Filecoin messages from a Mir batch.
func (m *Manager) GetMessages(batch *Batch) (msgs []*types.SignedMessage) {
	log.Infof("received a block with %d messages", len(msgs))
	for _, tx := range batch.Messages {

		input, err := parseTx(tx)
		if err != nil {
			log.Error("unable to decode a message in Mir block:", err)
			continue
		}

		switch msg := input.(type) {
		case *types.SignedMessage:
			// batch being processed, remove from mpool
			found := m.Pool.DeleteRequest(msg.Cid(), msg.Message.Nonce)
			if !found {
				log.Debugf("unable to find a message with %v hash in our local fifo.Pool", msg.Cid())
				// TODO: If we try to remove something from the pool, we should remember that
				// we already tried to remove that to avoid adding as it may lead to a dead-lock.
				// FIFO should be updated because we don't have the support for in-flight supports.
				// continue
			}
			msgs = append(msgs, msg)
			log.Infof("got message: to=%s, nonce= %d", msg.Message.To, msg.Message.Nonce)
		default:
			log.Error("got unknown message type in a block")
		}
	}
	return
}

func (m *Manager) TransportRequests(msgs []*types.SignedMessage) (
	requests []*mirproto.Request,
) {
	requests = append(requests, m.batchSignedMessages(msgs)...)
	return
}

func (m *Manager) ReconfigurationRequest(valset *ValidatorSet) *mirproto.Request {
	var payload bytes.Buffer
	if err := valset.MarshalCBOR(&payload); err != nil {
		log.Error("unable to marshall config valset:", err)
		return nil
	}
	r := mirproto.Request{
		ClientId: m.MirID,
		ReqNo:    m.reconfigurationNonce,
		Type:     ReconfigurationType,
		Data:     payload.Bytes(),
	}
	m.reconfigurationNonce++
	return &r
}

// batchPushSignedMessages pushes signed messages into the request pool and sends them to Mir.
func (m *Manager) batchSignedMessages(msgs []*types.SignedMessage) (
	requests []*mirproto.Request,
) {
	for _, msg := range msgs {
		clientID := msg.Message.From.String()
		nonce := msg.Message.Nonce
		if !m.Pool.IsTargetRequest(clientID, nonce) {
			log.Warnf("batchSignedMessage: target request not found for client ID")
			continue
		}

		data, err := MessageBytes(msg)
		if err != nil {
			log.Errorf("error in message bytes in batchSignedMessage: %s", err)
			continue
		}

		r := &mirproto.Request{
			ClientId: clientID,
			ReqNo:    nonce,
			Type:     TransportType,
			Data:     data,
		}

		m.Pool.AddRequest(msg.Cid(), r)

		requests = append(requests, r)
	}
	return requests
}

// ID prints Manager ID.
func (m *Manager) ID() string {
	return m.Addr.String()
}

// GetCheckpointPeriod returns the checkpoint period for the current epoch.
//
// The checkpoint period is computed as the number of validator times the
// segment length.
func (m *Manager) GetCheckpointPeriod() abi.ChainEpoch {
	return abi.ChainEpoch(m.segmentLength * len(m.StateManager.memberships[m.StateManager.currentEpoch]))
}

func (m *Manager) latestCheckpoint(params smr.Params) (*checkpoint.StableCheckpoint, error) {
	b, err := m.ds.Get(m.ctx, LatestCheckpointPbKey)
	if err != nil {
		if err == datastore.ErrNotFound {
			return smr.GenesisCheckpoint([]byte{}, params), nil
		}
		return nil, xerrors.Errorf("error getting latest snapshot: %w", err)
	}
	return checkpoint.DeserializeStableCheckpoint(b)
}
