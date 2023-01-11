package kit

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/go-statestore"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	mapi "github.com/filecoin-project/mir"
	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
	power3 "github.com/filecoin-project/specs-actors/v3/actors/builtin/power"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/power"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/gen"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/cmd/lotus-seed/seed"
	"github.com/filecoin-project/lotus/cmd/lotus-worker/sealworker"
	"github.com/filecoin-project/lotus/eudico-core/fxmodules"
	"github.com/filecoin-project/lotus/eudico-core/global"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/markets/idxprov"
	"github.com/filecoin-project/lotus/markets/idxprov/idxprov_test"
	lotusminer "github.com/filecoin-project/lotus/miner"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	testing2 "github.com/filecoin-project/lotus/node/modules/testing"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/paths"
	pipeline "github.com/filecoin-project/lotus/storage/pipeline"
	sectorstorage "github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/mock"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

func init() {
	chain.BootstrapPeerThreshold = 1
	messagepool.HeadChangeCoalesceMinDelay = time.Microsecond
	messagepool.HeadChangeCoalesceMaxDelay = 2 * time.Microsecond
	messagepool.HeadChangeCoalesceMergeInterval = 100 * time.Nanosecond
}

// Ensemble is a collection of nodes instantiated within a test.
//
// Create a new ensemble with:
//
//	ens := kit.NewEnsemble()
//
// Create full nodes and miners:
//
//	var full TestFullNode
//	var miner TestMiner
//	ens.FullNode(&full, opts...)       // populates a full node
//	ens.Miner(&miner, &full, opts...)  // populates a miner, using the full node as its chain daemon
//
// It is possible to pass functional options to set initial balances,
// presealed sectors, owner keys, etc.
//
// After the initial nodes are added, call `ens.Start()` to forge genesis
// and start the network. Mining will NOT be started automatically. It needs
// to be started explicitly by calling `BeginMining`.
//
// Nodes also need to be connected with one another, either via `ens.Connect()`
// or `ens.InterconnectAll()`. A common inchantation for simple tests is to do:
//
//	ens.InterconnectAll().BeginMining(blocktime)
//
// You can continue to add more nodes, but you must always follow with
// `ens.Start()` to activate the new nodes.
//
// The API is chainable, so it's possible to do a lot in a very succinct way:
//
//	kit.NewEnsemble().FullNode(&full).Miner(&miner, &full).Start().InterconnectAll().BeginMining()
//
// You can also find convenient fullnode:miner presets, such as 1:1, 1:2,
// and 2:1, e.g.:
//
//	kit.EnsembleMinimal()
//	kit.EnsembleOneTwo()
//	kit.EnsembleTwoOne()
type Ensemble struct {
	t            *testing.T
	bootstrapped bool
	genesisBlock bytes.Buffer
	mn           mocknet.Mocknet
	options      *ensembleOpts

	inactive struct {
		fullnodes []*TestFullNode
		miners    []*TestMiner
		workers   []*TestWorker
	}
	active struct {
		fullnodes []*TestFullNode
		miners    []*TestMiner
		workers   []*TestWorker
		bms       map[*TestMiner]*BlockMiner
	}
	genesis struct {
		version  network.Version
		miners   []genesis.Miner
		accounts []genesis.Actor
	}
}

// NewEnsemble instantiates a new blank Ensemble.
func NewEnsemble(t *testing.T, opts ...EnsembleOpt) *Ensemble {
	options := DefaultEnsembleOpts
	for _, o := range opts {
		err := o(&options)
		require.NoError(t, err)
	}

	n := &Ensemble{t: t, options: &options}
	n.active.bms = make(map[*TestMiner]*BlockMiner)

	for _, up := range options.upgradeSchedule {
		if up.Height < 0 {
			n.genesis.version = up.Network
		}
	}

	// add accounts from ensemble options to genesis.
	for _, acc := range options.accounts {
		n.genesis.accounts = append(n.genesis.accounts, genesis.Actor{
			Type:    genesis.TAccount,
			Balance: acc.initialBalance,
			Meta:    (&genesis.AccountMeta{Owner: acc.key.Address}).ActorMeta(),
		})
	}

	// Ensure we're using the right actors. This really shouldn't be some global thing, but it's
	// the best we can do for now.
	if n.options.mockProofs {
		require.NoError(t, build.UseNetworkBundle("testing-fake-proofs"))
	} else {
		require.NoError(t, build.UseNetworkBundle("testing"))
	}

	return n
}

// Mocknet returns the underlying mocknet.
func (n *Ensemble) Mocknet() mocknet.Mocknet {
	return n.mn
}

func (n *Ensemble) NewPrivKey() (libp2pcrypto.PrivKey, peer.ID) {
	privkey, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	require.NoError(n.t, err)

	peerId, err := peer.IDFromPrivateKey(privkey)
	require.NoError(n.t, err)

	return privkey, peerId
}

// FullNode enrolls a new full node.
func (n *Ensemble) FullNode(full *TestFullNode, opts ...NodeOpt) *Ensemble {
	options := DefaultNodeOpts
	for _, o := range opts {
		err := o(&options)
		require.NoError(n.t, err)
	}

	dkey, err := key.GenerateKey(types.KTBLS)
	// pow only supports only secpk keys. If we use bls keys it leads
	// to an error where for certain blocks there is the same ticket
	// for several miners, preventing the validation of certain blocks.
	// we would have to make some fixes if we want to use bls keys for pow
	if n.options.consensus == global.TSPoWConsensus {
		dkey, err = key.GenerateKey(types.KTSecp256k1)
	}
	require.NoError(n.t, err)

	if !n.bootstrapped && !options.balance.IsZero() {
		// if we still haven't forged genesis, create a key+address, and assign
		// it some FIL; this will be set as the default wallet when the node is
		// started.
		genacc := genesis.Actor{
			Type:    genesis.TAccount,
			Balance: options.balance,
			Meta:    (&genesis.AccountMeta{Owner: dkey.Address}).ActorMeta(),
		}

		n.genesis.accounts = append(n.genesis.accounts, genacc)
	}

	*full = TestFullNode{t: n.t, options: options, DefaultKey: dkey}

	n.inactive.fullnodes = append(n.inactive.fullnodes, full)
	return n
}

// MinerEnroll enrolls a new miner, using the provided full node for chain
// interactions.
func (n *Ensemble) MinerEnroll(minerNode *TestMiner, full *TestFullNode, opts ...NodeOpt) *Ensemble {
	require.NotNil(n.t, full, "full node required when instantiating miner")

	options := DefaultNodeOpts
	for _, o := range opts {
		err := o(&options)
		require.NoError(n.t, err)
	}

	privkey, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	require.NoError(n.t, err)

	peerId, err := peer.IDFromPrivateKey(privkey)
	require.NoError(n.t, err)

	tdir, err := ioutil.TempDir("", "preseal-memgen")
	require.NoError(n.t, err)

	minerCnt := len(n.inactive.miners) + len(n.active.miners)

	actorAddr, err := address.NewIDAddress(genesis2.MinerStart + uint64(minerCnt))
	require.NoError(n.t, err)

	if options.mainMiner != nil {
		actorAddr = options.mainMiner.ActorAddr
	}

	ownerKey := options.ownerKey
	var presealSectors int

	if !n.bootstrapped {
		presealSectors = options.sectors

		var (
			k    *types.KeyInfo
			genm *genesis.Miner
		)

		// Will use 2KiB sectors by default (default value of sectorSize).
		proofType, err := miner.SealProofTypeFromSectorSize(options.sectorSize, n.genesis.version)
		require.NoError(n.t, err)

		// Create the preseal commitment.
		if n.options.mockProofs {
			genm, k, err = mock.PreSeal(proofType, actorAddr, presealSectors)
		} else {
			genm, k, err = seed.PreSeal(actorAddr, proofType, 0, presealSectors, tdir, []byte("make genesis mem random"), nil, true)
		}
		require.NoError(n.t, err)

		genm.PeerId = peerId

		// create an owner key, and assign it some FIL.
		ownerKey, err = key.NewKey(*k)
		require.NoError(n.t, err)

		genacc := genesis.Actor{
			Type:    genesis.TAccount,
			Balance: options.balance,
			Meta:    (&genesis.AccountMeta{Owner: ownerKey.Address}).ActorMeta(),
		}

		n.genesis.miners = append(n.genesis.miners, *genm)
		n.genesis.accounts = append(n.genesis.accounts, genacc)
	} else {
		require.NotNil(n.t, ownerKey, "worker key can't be null if initializing a miner after genesis")
	}

	rl, err := net.Listen("tcp", "127.0.0.1:")
	require.NoError(n.t, err)

	*minerNode = TestMiner{
		t:              n.t,
		ActorAddr:      actorAddr,
		OwnerKey:       ownerKey,
		FullNode:       full,
		PresealDir:     tdir,
		PresealSectors: presealSectors,
		options:        options,
		RemoteListener: rl,
	}

	minerNode.Libp2p.PeerID = peerId
	minerNode.Libp2p.PrivKey = privkey

	return n
}

func (n *Ensemble) AddInactiveMiner(m *TestMiner) {
	n.inactive.miners = append(n.inactive.miners, m)
}

func (n *Ensemble) Miner(minerNode *TestMiner, full *TestFullNode, opts ...NodeOpt) *Ensemble {
	n.MinerEnroll(minerNode, full, opts...)
	n.AddInactiveMiner(minerNode)
	return n
}

// Worker enrolls a new worker, using the provided full node for chain
// interactions.
func (n *Ensemble) Worker(minerNode *TestMiner, worker *TestWorker, opts ...NodeOpt) *Ensemble {
	require.NotNil(n.t, minerNode, "miner node required when instantiating worker")

	options := DefaultNodeOpts
	for _, o := range opts {
		err := o(&options)
		require.NoError(n.t, err)
	}

	rl, err := net.Listen("tcp", "127.0.0.1:")
	require.NoError(n.t, err)

	*worker = TestWorker{
		t:              n.t,
		MinerNode:      minerNode,
		RemoteListener: rl,
		options:        options,
	}

	n.inactive.workers = append(n.inactive.workers, worker)

	return n
}

// EnsembleFullNodeOnly creates and starts a EudicoEnsemble with a single full node.
// It is used with Eudico consensus protocols that implementation is different from Filecoin one.
func EnsembleFullNodeOnly(t *testing.T, opts ...interface{}) (*TestFullNode, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full TestFullNode
	)
	ens := NewEnsemble(t, eopts...).FullNode(&full, nopts...).Start()
	return &full, ens
}

// Start starts all enrolled nodes.
func (n *Ensemble) Start() *Ensemble {
	ctx := context.Background()

	var gtempl *genesis.Template
	if !n.bootstrapped {
		// We haven't been bootstrapped yet, we need to generate genesis and
		// create the networking backbone.
		gtempl = n.generateGenesis()
		n.mn = mocknet.New()
	}

	// ---------------------
	//  FULL NODES
	// ---------------------

	// Create all inactive full nodes.
	for i, full := range n.inactive.fullnodes {

		var r repo.Repo
		if !full.options.fsrepo {
			rmem := repo.NewMemory(nil)
			n.t.Cleanup(rmem.Cleanup)
			r = rmem
		} else {
			repoPath := n.t.TempDir()
			rfs, err := repo.NewFS(repoPath)
			require.NoError(n.t, err)
			require.NoError(n.t, rfs.Init(repo.FullNode))
			r = rfs
		}

		// setup config with options
		lr, err := r.Lock(repo.FullNode)
		require.NoError(n.t, err)

		ks, err := lr.KeyStore()
		require.NoError(n.t, err)

		if full.Pkey != nil {
			pk, err := libp2pcrypto.MarshalPrivateKey(full.Pkey.PrivKey)
			require.NoError(n.t, err)

			err = ks.Put("libp2p-host", types.KeyInfo{
				Type:       "libp2p-host",
				PrivateKey: pk,
			})
			require.NoError(n.t, err)
		}

		c, err := lr.Config()
		require.NoError(n.t, err)

		cfg, ok := c.(*config.FullNode)
		if !ok {
			n.t.Fatalf("invalid config from repo, got: %T", c)
		}
		for _, opt := range full.options.cfgOpts {
			require.NoError(n.t, opt(cfg))
		}
		err = lr.SetConfig(func(raw interface{}) {
			rcfg := raw.(*config.FullNode)
			*rcfg = *cfg
		})
		require.NoError(n.t, err)

		genesisProvider := fx.Options()
		if i == 0 && !n.bootstrapped {
			genesisProvider = fx.Provide(testing2.MakeGenesisMem(&n.genesisBlock, *gtempl))
		} else {
			genesisProvider = fx.Provide(modules.LoadGenesis(n.genesisBlock.Bytes()))
		}

		fxProviders := fx.Options(
			fxmodules.Fullnode(false, full.options.lite),
			fxmodules.Libp2p(&cfg.Common),
			fxmodules.Repository(lr, cfg),
			fxmodules.Blockstore(cfg),
			fxmodules.Consensus(n.options.consensus),
			// misc providers
			fx.Supply(dtypes.Bootstrapper(true)),
			fx.Supply(dtypes.ShutdownChan(make(chan struct{}))),
			genesisProvider,
			fx.Replace(n.options.upgradeSchedule),
			fx.Decorate(testing2.RandomBeacon),
		)

		app := fx.New(
			fxProviders,
			fxmodules.Invokes(&cfg.Common, false, n.options.consensus == global.MirConsensus && !full.options.learner),
			fx.Invoke(func(fullNode impl.FullNodeAPI) {
				full.FullNode = &fullNode
			}),
			fx.NopLogger,
		)
		err = app.Start(ctx)
		require.NoError(n.t, err)

		stop := app.Stop
		full.Stop = stop

		addr, err := full.WalletImport(context.Background(), &full.DefaultKey.KeyInfo)
		require.NoError(n.t, err)

		err = full.WalletSetDefault(context.Background(), addr)
		require.NoError(n.t, err)

		var rpcShutdownOnce sync.Once
		var stopOnce sync.Once
		var stopErr error

		stopFunc := stop
		stop = func(ctx context.Context) error {
			stopOnce.Do(func() {
				stopErr = stopFunc(ctx)
			})
			return stopErr
		}

		// Are we hitting this node through its RPC?
		if full.options.rpc {
			withRPC, rpcCloser := fullRpc(n.t, full)
			n.inactive.fullnodes[i] = withRPC
			full.Stop = func(ctx2 context.Context) error {
				rpcShutdownOnce.Do(rpcCloser)
				return stop(ctx)
			}
			n.t.Cleanup(func() { rpcShutdownOnce.Do(rpcCloser) })
		}

		n.t.Cleanup(func() {
			_ = stop(context.Background())
		})

		n.active.fullnodes = append(n.active.fullnodes, full)
	}

	// If we are here, we have processed all inactive fullnodes and moved them
	// to active, so clear the slice.
	n.inactive.fullnodes = n.inactive.fullnodes[:0]

	// Link all the nodes.
	err := n.mn.LinkAll()
	require.NoError(n.t, err)

	// ---------------------
	//  MINERS
	// ---------------------

	// Create all inactive miners.
	for i, m := range n.inactive.miners {
		if n.bootstrapped {
			if m.options.mainMiner == nil {
				// this is a miner created after genesis, so it won't have a preseal.
				// we need to create it on chain.

				proofType, err := miner.WindowPoStProofTypeFromSectorSize(m.options.sectorSize)
				require.NoError(n.t, err)

				params, aerr := actors.SerializeParams(&power3.CreateMinerParams{
					Owner:               m.OwnerKey.Address,
					Worker:              m.OwnerKey.Address,
					WindowPoStProofType: proofType,
					Peer:                abi.PeerID(m.Libp2p.PeerID),
				})
				require.NoError(n.t, aerr)

				createStorageMinerMsg := &types.Message{
					From:  m.OwnerKey.Address,
					To:    power.Address,
					Value: big.Zero(),

					Method: power.Methods.CreateMiner,
					Params: params,
				}
				signed, err := m.FullNode.FullNode.MpoolPushMessage(ctx, createStorageMinerMsg, &api.MessageSendSpec{
					MsgUuid: uuid.New(),
				})
				require.NoError(n.t, err)

				mw, err := m.FullNode.FullNode.StateWaitMsg(ctx, signed.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
				require.NoError(n.t, err)
				require.Equal(n.t, exitcode.Ok, mw.Receipt.ExitCode)

				var retval power3.CreateMinerReturn
				err = retval.UnmarshalCBOR(bytes.NewReader(mw.Receipt.Return))
				require.NoError(n.t, err, "failed to create miner")

				m.ActorAddr = retval.IDAddress
			} else {
				params, err := actors.SerializeParams(&miner2.ChangePeerIDParams{NewID: abi.PeerID(m.Libp2p.PeerID)})
				require.NoError(n.t, err)

				msg := &types.Message{
					To:     m.options.mainMiner.ActorAddr,
					From:   m.options.mainMiner.OwnerKey.Address,
					Method: builtin.MethodsMiner.ChangePeerID,
					Params: params,
					Value:  types.NewInt(0),
				}

				signed, err2 := m.FullNode.FullNode.MpoolPushMessage(ctx, msg, &api.MessageSendSpec{
					MsgUuid: uuid.New(),
				})
				require.NoError(n.t, err2)

				mw, err2 := m.FullNode.FullNode.StateWaitMsg(ctx, signed.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
				require.NoError(n.t, err2)
				require.Equal(n.t, exitcode.Ok, mw.Receipt.ExitCode)
			}
		}

		has, err := m.FullNode.WalletHas(ctx, m.OwnerKey.Address)
		require.NoError(n.t, err)

		// Only import the owner's full key into our companion full node, if we
		// don't have it still.
		if !has {
			_, err = m.FullNode.WalletImport(ctx, &m.OwnerKey.KeyInfo)
			require.NoError(n.t, err)
		}

		// // Set it as the default address.
		// err = m.FullNode.WalletSetDefault(ctx, m.OwnerAddr.Address)
		// require.NoError(n.t, err)

		r := repo.NewMemory(nil)
		n.t.Cleanup(r.Cleanup)

		lr, err := r.Lock(repo.StorageMiner)
		require.NoError(n.t, err)

		c, err := lr.Config()
		require.NoError(n.t, err)

		cfg, ok := c.(*config.StorageMiner)
		if !ok {
			n.t.Fatalf("invalid config from repo, got: %T", c)
		}
		cfg.Common.API.RemoteListenAddress = m.RemoteListener.Addr().String()
		cfg.Subsystems.EnableMarkets = m.options.subsystems.Has(SMarkets)
		cfg.Subsystems.EnableMining = m.options.subsystems.Has(SMining)
		cfg.Subsystems.EnableSealing = m.options.subsystems.Has(SSealing)
		cfg.Subsystems.EnableSectorStorage = m.options.subsystems.Has(SSectorStorage)
		cfg.Dealmaking.MaxStagingDealsBytes = m.options.maxStagingDealsBytes

		if m.options.mainMiner != nil {
			token, err := m.options.mainMiner.FullNode.AuthNew(ctx, api.AllPermissions)
			require.NoError(n.t, err)

			cfg.Subsystems.SectorIndexApiInfo = fmt.Sprintf("%s:%s", token, m.options.mainMiner.ListenAddr)
			cfg.Subsystems.SealerApiInfo = fmt.Sprintf("%s:%s", token, m.options.mainMiner.ListenAddr)
		}

		err = lr.SetConfig(func(raw interface{}) {
			rcfg := raw.(*config.StorageMiner)
			*rcfg = *cfg
		})
		require.NoError(n.t, err)

		ks, err := lr.KeyStore()
		require.NoError(n.t, err)

		pk, err := libp2pcrypto.MarshalPrivateKey(m.Libp2p.PrivKey)
		require.NoError(n.t, err)

		err = ks.Put("libp2p-host", types.KeyInfo{
			Type:       "libp2p-host",
			PrivateKey: pk,
		})
		require.NoError(n.t, err)

		ds, err := lr.Datastore(context.TODO(), "/metadata")
		require.NoError(n.t, err)

		err = ds.Put(ctx, datastore.NewKey("miner-address"), m.ActorAddr.Bytes())
		require.NoError(n.t, err)

		if i < len(n.genesis.miners) && !n.bootstrapped {
			// if this is a genesis miner, import preseal metadata
			require.NoError(n.t, importPreSealMeta(ctx, n.genesis.miners[i], ds))
		}

		// using real proofs, therefore need real sectors.
		if !n.bootstrapped && !n.options.mockProofs {
			psd := m.PresealDir
			noPaths := m.options.noStorage

			err := lr.SetStorage(func(sc *storiface.StorageConfig) {
				if noPaths {
					sc.StoragePaths = []storiface.LocalPath{}
				}
				sc.StoragePaths = append(sc.StoragePaths, storiface.LocalPath{Path: psd})
			})

			require.NoError(n.t, err)
		}

		err = lr.Close()
		require.NoError(n.t, err)

		if m.options.mainMiner == nil {
			enc, err := actors.SerializeParams(&miner2.ChangePeerIDParams{NewID: abi.PeerID(m.Libp2p.PeerID)})
			require.NoError(n.t, err)

			msg := &types.Message{
				From:   m.OwnerKey.Address,
				To:     m.ActorAddr,
				Method: builtin.MethodsMiner.ChangePeerID,
				Params: enc,
				Value:  types.NewInt(0),
			}

			_, err2 := m.FullNode.MpoolPushMessage(ctx, msg, &api.MessageSendSpec{
				MsgUuid: uuid.New(),
			})
			require.NoError(n.t, err2)
		}

		noLocal := m.options.minerNoLocalSealing
		assigner := m.options.minerAssigner
		disallowRemoteFinalize := m.options.disallowRemoteFinalize

		var mineBlock = make(chan lotusminer.MineReq)

		copy := *m.FullNode
		copy.FullNode = modules.MakeUuidWrapper(copy.FullNode)
		m.FullNode = &copy

		// m.FullNode.FullNode = modules.MakeUuidWrapper(fn.FullNode)

		opts := []node.Option{
			node.StorageMiner(&m.StorageMiner, cfg.Subsystems),
			node.Base(),
			node.Repo(r),
			node.Test(),

			node.If(m.options.disableLibp2p, node.MockHost(n.mn)),
			// node.Override(new(v1api.RawFullNodeAPI), func() api.FullNode { return modules.MakeUuidWrapper(m.FullNode) }),
			// node.Override(new(v1api.RawFullNodeAPI), modules.MakeUuidWrapper),
			node.Override(new(v1api.RawFullNodeAPI), m.FullNode),
			node.Override(new(*lotusminer.Miner), lotusminer.NewTestMiner(mineBlock, m.ActorAddr)),

			// disable resource filtering so that local worker gets assigned tasks
			// regardless of system pressure.
			node.Override(new(config.SealerConfig), func() config.SealerConfig {
				scfg := config.DefaultStorageMiner()

				if noLocal {
					scfg.Storage.AllowSectorDownload = false
					scfg.Storage.AllowAddPiece = false
					scfg.Storage.AllowPreCommit1 = false
					scfg.Storage.AllowPreCommit2 = false
					scfg.Storage.AllowCommit = false
					scfg.Storage.AllowUnseal = false
				}

				scfg.Storage.Assigner = assigner
				scfg.Storage.DisallowRemoteFinalize = disallowRemoteFinalize
				scfg.Storage.ResourceFiltering = config.ResourceFilteringDisabled
				return scfg.Storage
			}),

			// upgrades
			node.Override(new(stmgr.UpgradeSchedule), n.options.upgradeSchedule),
		}

		if m.options.subsystems.Has(SMarkets) {
			opts = append(opts,
				node.Override(new(idxprov.MeshCreator), idxprov_test.NewNoopMeshCreator),
			)
		}

		// append any node builder options.
		opts = append(opts, m.options.extraNodeOpts...)

		idAddr, err := address.IDFromAddress(m.ActorAddr)
		require.NoError(n.t, err)

		// preload preseals if the network still hasn't bootstrapped.
		var presealSectors []abi.SectorID
		if !n.bootstrapped {
			sectors := n.genesis.miners[i].Sectors
			for _, sector := range sectors {
				presealSectors = append(presealSectors, abi.SectorID{
					Miner:  abi.ActorID(idAddr),
					Number: sector.SectorID,
				})
			}
		}

		if n.options.mockProofs {
			opts = append(opts,
				node.Override(new(*mock.SectorMgr), func() (*mock.SectorMgr, error) {
					return mock.NewMockSectorMgr(presealSectors), nil
				}),
				node.Override(new(sectorstorage.SectorManager), node.From(new(*mock.SectorMgr))),
				node.Override(new(sectorstorage.Unsealer), node.From(new(*mock.SectorMgr))),
				node.Override(new(sectorstorage.PieceProvider), node.From(new(*mock.SectorMgr))),

				node.Override(new(storiface.Verifier), mock.MockVerifier),
				node.Override(new(storiface.Prover), mock.MockProver),
				node.Unset(new(*sectorstorage.Manager)),
			)
		}

		// start node
		stop, err := node.New(ctx, opts...)
		require.NoError(n.t, err)

		n.t.Cleanup(func() { _ = stop(context.Background()) })

		m.BaseAPI = m.StorageMiner

		// Are we hitting this node through its RPC?
		if m.options.rpc {
			withRPC := minerRpc(n.t, m)
			n.inactive.miners[i] = withRPC
		}

		mineOne := func(ctx context.Context, req lotusminer.MineReq) error {
			select {
			case mineBlock <- req:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		m.MineOne = mineOne
		m.Stop = stop

		n.active.miners = append(n.active.miners, m)
	}

	// If we are here, we have processed all inactive miners and moved them
	// to active, so clear the slice.
	n.inactive.miners = n.inactive.miners[:0]

	// ---------------------
	//  WORKERS
	// ---------------------

	// Create all inactive workers.
	for i, m := range n.inactive.workers {
		r := repo.NewMemory(nil)

		lr, err := r.Lock(repo.Worker)
		require.NoError(n.t, err)

		if m.options.noStorage {
			err := lr.SetStorage(func(sc *storiface.StorageConfig) {
				sc.StoragePaths = []storiface.LocalPath{}
			})
			require.NoError(n.t, err)
		}

		ds, err := lr.Datastore(context.Background(), "/metadata")
		require.NoError(n.t, err)

		addr := m.RemoteListener.Addr().String()

		localStore, err := paths.NewLocal(ctx, lr, m.MinerNode, []string{"http://" + addr + "/remote"})
		require.NoError(n.t, err)

		auth := http.Header(nil)

		remote := paths.NewRemote(localStore, m.MinerNode, auth, 20, &paths.DefaultPartialFileHandler{})
		store := m.options.workerStorageOpt(remote)

		fh := &paths.FetchHandler{Local: localStore, PfHandler: &paths.DefaultPartialFileHandler{}}
		m.FetchHandler = fh.ServeHTTP

		wsts := statestore.New(namespace.Wrap(ds, modules.WorkerCallsPrefix))

		workerApi := &sealworker.Worker{
			LocalWorker: sectorstorage.NewLocalWorker(sectorstorage.WorkerConfig{
				TaskTypes: m.options.workerTasks,
				NoSwap:    false,
				Name:      m.options.workerName,
			}, store, localStore, m.MinerNode, m.MinerNode, wsts),
			LocalStore: localStore,
			Storage:    lr,
		}

		m.Worker = workerApi

		require.True(n.t, m.options.rpc)

		withRPC := workerRpc(n.t, m)
		n.inactive.workers[i] = withRPC

		err = m.MinerNode.WorkerConnect(ctx, "http://"+addr+"/rpc/v0")
		require.NoError(n.t, err)

		n.active.workers = append(n.active.workers, m)
	}

	// If we are here, we have processed all inactive workers and moved them
	// to active, so clear the slice.
	n.inactive.workers = n.inactive.workers[:0]

	// ---------------------
	//  MISC
	// ---------------------

	// Link all the nodes.
	err = n.mn.LinkAll()
	require.NoError(n.t, err)

	if global.IsConsensusAlgorithm(global.ExpectedConsensus) &&
		!n.bootstrapped && len(n.active.miners) > 0 {
		// We have *just* bootstrapped, so mine 2 blocks to setup some CE stuff in some actors
		var wait sync.Mutex
		wait.Lock()

		observer := n.active.fullnodes[0]

		bm := NewBlockMiner(n.t, n.active.miners[0])
		n.t.Cleanup(bm.Stop)

		bm.MineUntilBlock(ctx, observer, func(epoch abi.ChainEpoch) {
			wait.Unlock()
		})

		wait.Lock()
		bm.MineUntilBlock(ctx, observer, func(epoch abi.ChainEpoch) {
			wait.Unlock()
		})
		wait.Lock()
		n.bootstrapped = true
	}

	return n
}

// InterconnectAll connects all miners and full nodes to one another.
func (n *Ensemble) InterconnectAll() *Ensemble {
	// connect full nodes to miners.
	for _, from := range n.active.fullnodes {
		for _, to := range n.active.miners {
			// []*TestMiner to []api.CommonAPI type coercion not possible
			// so cannot use variadic form.
			n.Connect(from, to)
		}
	}

	// connect full nodes between each other, skipping ourselves.
	last := len(n.active.fullnodes) - 1
	for i, from := range n.active.fullnodes {
		if i == last {
			continue
		}
		for _, to := range n.active.fullnodes[i+1:] {
			n.Connect(from, to)
		}
	}
	return n
}

// InterconnectFullNodes connects full nodes to one another.
func (n *Ensemble) InterconnectFullNodes() *Ensemble {
	// connect full nodes between each other, skipping ourselves.
	last := len(n.active.fullnodes) - 1
	for i, from := range n.active.fullnodes {
		if i == last {
			continue
		}
		for _, to := range n.active.fullnodes[i+1:] {
			n.Connect(from, to)
		}
	}
	return n
}

// Connect connects one full node to the provided full nodes.
func (n *Ensemble) Connect(from api.Net, to ...api.Net) *Ensemble {
	addr, err := from.NetAddrsListen(context.Background())
	require.NoError(n.t, err)

	for _, other := range to {
		err = other.NetConnect(context.Background(), addr)
		require.NoError(n.t, err)
	}
	return n
}

// Disconnect disconnects one full from the provided full nodes.
func (n *Ensemble) Disconnect(from api.Net, to ...api.Net) *Ensemble {
	addr, err := from.NetAddrsListen(context.Background())
	require.NoError(n.t, err)

	for _, other := range to {
		err = other.NetDisconnect(context.Background(), addr.ID)
		require.NoError(n.t, err)
	}
	return n
}

// DisconnectNodes disconnects one full from the provided full nodes.
func (n *Ensemble) DisconnectNodes(from api.Net, to ...*TestFullNode) *Ensemble {
	addr, err := from.NetAddrsListen(context.Background())
	require.NoError(n.t, err)

	for _, other := range to {
		err = other.NetDisconnect(context.Background(), addr.ID)
		require.NoError(n.t, err)
	}
	return n
}

// DisconnectMirMiners disconnects Mir miners.
func (n *Ensemble) DisconnectMirMiners(faultyMiners []*TestMiner) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	for _, faultyMiner := range faultyMiners {
		go func(ctx context.Context, m *TestMiner) {
			for {
				select {
				case <-ctx.Done():
					return
				default:

				}
				conns := m.mirHost.Network().Conns()
				for _, c := range conns {
					err := c.Close()
					require.NoError(n.t, err)
				}
			}
		}(ctx, faultyMiner)
	}

	return cancel
}

func (n *Ensemble) BeginMiningMustPost(blocktime time.Duration, miners ...*TestMiner) []*BlockMiner {
	ctx := context.Background()

	// wait one second to make sure that nodes are connected and have handshaken.
	// TODO make this deterministic by listening to identify events on the
	//  libp2p eventbus instead (or something else).
	time.Sleep(1 * time.Second)

	var bms []*BlockMiner
	if len(miners) == 0 {
		// no miners have been provided explicitly, instantiate block miners
		// for all active miners that aren't still mining.
		for _, m := range n.active.miners {
			if _, ok := n.active.bms[m]; ok {
				continue // skip, already have a block miner
			}
			miners = append(miners, m)
		}
	}

	if len(miners) > 1 {
		n.t.Fatalf("Only one active miner for MustPost, but have %d", len(miners))
	}

	for _, m := range miners {
		bm := NewBlockMiner(n.t, m)
		bm.MineBlocksMustPost(ctx, blocktime)
		n.t.Cleanup(bm.Stop)

		bms = append(bms, bm)

		n.active.bms[m] = bm
	}

	return bms
}

// BeginMining kicks off mining for the specified miners. If nil or 0-length,
// it will kick off mining for all enrolled and active miners. It also adds a
// cleanup function to stop all mining operations on test teardown.
func (n *Ensemble) BeginMining(blocktime time.Duration, miners ...*TestMiner) []*BlockMiner {
	ctx := context.Background()

	// wait one second to make sure that nodes are connected and have handshaken.
	// TODO make this deterministic by listening to identify events on the
	//  libp2p eventbus instead (or something else).
	time.Sleep(1 * time.Second)

	var bms []*BlockMiner
	if len(miners) == 0 {
		// no miners have been provided explicitly, instantiate block miners
		// for all active miners that aren't still mining.
		for _, m := range n.active.miners {
			if _, ok := n.active.bms[m]; ok {
				continue // skip, already have a block miner
			}
			miners = append(miners, m)
		}
	}

	for _, m := range miners {
		bm := NewBlockMiner(n.t, m)
		bm.MineBlocks(ctx, blocktime)
		n.t.Cleanup(bm.Stop)

		bms = append(bms, bm)

		n.active.bms[m] = bm
	}

	return bms
}

func (n *Ensemble) mirMembership(miners ...*TestMiner) string {
	var membership string
	for _, m := range miners {
		id, err := NodeLibp2pAddr(m.mirHost)
		require.NoError(n.t, err)
		membership += fmt.Sprintf("%s@%s,", m.mirAddr, id)
	}
	return membership
}

func (n *Ensemble) BeginMirMiningWithDelay(ctx context.Context, wg *sync.WaitGroup, delay int, miners ...*TestMiner) {
	n.BeginMirMiningWithDelayForFaultyNodes(ctx, wg, delay, miners)
}

func (n *Ensemble) BeginMirMining(ctx context.Context, wg *sync.WaitGroup, miners ...*TestMiner) {
	n.BeginMirMiningWithDelay(ctx, wg, 0, miners...)
	// once validators start mining mark the network as bootstrapped so no
	// new genesis are generated.
	n.Bootstrapped()
}

// Bootstrap explicitly sets the ensemble as bootstrapped.
func (n *Ensemble) Bootstrapped() {
	n.bootstrapped = true
}

func (n *Ensemble) BeginMirMiningWithDelayForFaultyNodes(ctx context.Context, wg *sync.WaitGroup, delay int, miners []*TestMiner, faultyMiners ...*TestMiner) {
	membership := n.mirMembership(append(miners, faultyMiners...)...)

	for i, m := range append(miners, faultyMiners...) {
		ctx, cancel := context.WithCancel(ctx)
		m.stopMir = cancel

		wg.Add(1)

		go func(ctx context.Context, i int, m *TestMiner) {
			defer wg.Done()
			m.mirMembership = membership
			m.mirDB = NewTestDB()
			m.checkpointPeriod = len(miners) + len(faultyMiners)
			cfg := mir.Config{
				MembershipCfg:    mir.MembershipFromStr(membership),
				CheckpointPeriod: m.checkpointPeriod,
			}
			if i > len(miners) && delay > 0 {
				RandomDelay(delay)
			}
			err := mir.Mine(ctx, m.mirAddr, m.mirHost, m.FullNode, m.mirDB, &cfg)
			if xerrors.Is(mapi.ErrStopped, err) {
				return
			}
			require.NoError(n.t, err)
		}(ctx, i, m)
	}
}

func (n *Ensemble) BeginPoWMining(ctx context.Context, wg *sync.WaitGroup, miners ...*TestMiner) {

	for i, m := range miners {
		ctx, cancel := context.WithCancel(ctx)
		m.stopMir = cancel

		wg.Add(1)

		go func(ctx context.Context, i int, m *TestMiner) {
			defer wg.Done()
			err := tspow.Mine(ctx, m.FullNode.DefaultKey.Address, m.FullNode)
			if xerrors.Is(mapi.ErrStopped, err) {
				return
			}
			require.NoError(n.t, err)
		}(ctx, i, m)
	}
}

func (n *Ensemble) RestoreMirMinersWithOptions(ctx context.Context, withPersistentDB bool, miners ...*TestMiner) {
	for _, m := range miners {
		if withPersistentDB && m.mirDB == nil {
			n.t.Fatalf("nil miner database: %v", m.mirAddr)
		}
		if m.mirMembership == "" {
			n.t.Fatalf("empty miner membership: %v", m.mirAddr)
		}
		if m.checkpointPeriod <= 0 {
			n.t.Fatalf("invalid checkpoint period: %v", m.checkpointPeriod)
		}
		go func(m *TestMiner) {
			var err error
			// recover host with original config
			m.mirHost, err = libp2p.New(
				libp2p.Identity(m.mirPrivKey),
				libp2p.DefaultTransports,
				libp2p.ListenAddrs(m.mirMultiAddr...),
			)
			if !withPersistentDB {
				m.mirDB = NewTestDB()
			}
			cfg := mir.Config{
				MembershipCfg:    mir.MembershipFromStr(m.mirMembership),
				CheckpointPeriod: m.checkpointPeriod,
			}
			err = mir.Mine(ctx, m.mirAddr, m.mirHost, m.FullNode, m.mirDB, &cfg)
			if xerrors.Is(mapi.ErrStopped, err) {
				return
			}
			require.NoError(n.t, err)
		}(m)
	}
}

func (n *Ensemble) RestoreMirMinersWithEmptyState(ctx context.Context, miners ...*TestMiner) {
	n.RestoreMirMinersWithOptions(ctx, false, miners...)
}

func (n *Ensemble) RestoreMirMinersWithState(ctx context.Context, miners ...*TestMiner) {
	n.RestoreMirMinersWithOptions(ctx, true, miners...)
}

func (n *Ensemble) CrashMirMiners(ctx context.Context, delay int, miners ...*TestMiner) {
	for _, m := range miners {
		m.stopMir()
		// FIXME: Mir won't close the transport correctly,
		// and if we crash the node we need to close the libp2p
		// host to prevent other mir validators from considering
		// the crash validator as "online".
		// m.mirHost.Close()
		if delay > 0 {
			RandomDelay(delay)
		}
	}
}

func (n *Ensemble) generateGenesis() *genesis.Template {
	var verifRoot = gen.DefaultVerifregRootkeyActor
	if k := n.options.verifiedRoot.key; k != nil {
		verifRoot = genesis.Actor{
			Type:    genesis.TAccount,
			Balance: n.options.verifiedRoot.initialBalance,
			Meta:    (&genesis.AccountMeta{Owner: k.Address}).ActorMeta(),
		}
	}

	templ := &genesis.Template{
		NetworkVersion:   n.genesis.version,
		Accounts:         n.genesis.accounts,
		Miners:           n.genesis.miners,
		NetworkName:      "test",
		Timestamp:        uint64(time.Now().Unix() - int64(n.options.pastOffset.Seconds())),
		VerifregRootKey:  verifRoot,
		RemainderAccount: gen.DefaultRemainderAccountActor,
	}

	return templ
}

func importPreSealMeta(ctx context.Context, meta genesis.Miner, mds dtypes.MetadataDS) error {
	maxSectorID := abi.SectorNumber(0)
	for _, sector := range meta.Sectors {
		sectorKey := datastore.NewKey(pipeline.SectorStorePrefix).ChildString(fmt.Sprint(sector.SectorID))

		commD := sector.CommD
		commR := sector.CommR

		info := &pipeline.SectorInfo{
			State:        pipeline.Proving,
			SectorNumber: sector.SectorID,
			Pieces: []api.SectorPiece{
				{
					Piece: abi.PieceInfo{
						Size:     abi.PaddedPieceSize(meta.SectorSize),
						PieceCID: commD,
					},
					DealInfo: nil, // todo: likely possible to get, but not really that useful
				},
			},
			CommD: &commD,
			CommR: &commR,
		}

		b, err := cborutil.Dump(info)
		if err != nil {
			return err
		}

		if err := mds.Put(ctx, sectorKey, b); err != nil {
			return err
		}

		if sector.SectorID > maxSectorID {
			maxSectorID = sector.SectorID
		}
	}

	buf := make([]byte, binary.MaxVarintLen64)
	size := binary.PutUvarint(buf, uint64(maxSectorID))
	return mds.Put(ctx, datastore.NewKey(pipeline.StorageCounterDSPrefix), buf[:size])
}
