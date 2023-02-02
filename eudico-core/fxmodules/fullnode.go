package fxmodules

import (
	"context"
	"time"

	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/node/repo"

	metricsi "github.com/ipfs/go-metrics-interface"
	"go.uber.org/fx"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/exchange"
	"github.com/filecoin-project/lotus/chain/market"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/messagesigner"
	"github.com/filecoin-project/lotus/chain/stmgr"
	rpcstmgr "github.com/filecoin-project/lotus/chain/stmgr/rpc"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/journal/alerting"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/markets/retrievaladapter"
	"github.com/filecoin-project/lotus/markets/storageadapter"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/hello"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	"github.com/filecoin-project/lotus/paychmgr"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

func Fullnode(isBootstrap bool, isLite bool, fevmCfg config.FevmConfig) fx.Option {
	var nodeAPIProviders fx.Option
	if isLite {
		nodeAPIProviders = liteNodeAPIProviders
	} else {
		nodeAPIProviders = fullNodeAPIProviders
	}

	return fx.Module("fullnode",
		nodeAPIProviders,
		// Consensus: crypto dependencies
		fx.Supply(
			fx.Annotate(
				ffiwrapper.ProofVerifier,
				fx.As(new(storiface.Verifier)),
			),
			fx.Annotate(
				ffiwrapper.ProofProver,
				fx.As(new(storiface.Prover)),
			),
			// We don't want the SyncManagerCtor to be used as an fx constructor, but rather as a value.
			// It will be called implicitly by the Syncer constructor.
			chain.SyncManagerCtor(chain.NewSyncManager),

			new(dtypes.MpoolLocker),
		),
		// Eth APIs
		fx.Provide(
			modules.EthEventAPI(fevmCfg),
			func(event *full.EthEvent) full.EthEventAPI { return event },
		),
		fxEitherOr(
			fevmCfg.EnableEthRPC,
			fx.Provide(
				// You may not like it, but this is what peak performance looks like
				func(mctx helpers.MetricsCtx, r repo.LockedRepo, lc fx.Lifecycle, cs *store.ChainStore, sm *stmgr.StateManager, evapi modules.EventAPI, mp *messagepool.MessagePool, stateapi full.StateAPI, chainapi full.ChainAPI, mpoolapi full.MpoolAPI) (full.EthModuleAPI, error) {
					return modules.EthModuleAPI(fevmCfg)(mctx, r, lc, cs, sm, evapi, mp, stateapi, chainapi, mpoolapi)
				},
			),
			fx.Provide(func() full.EthModuleAPI { return &full.EthModuleDummy{} }),
		),
		// bootstrap settings
		fxOptional(isBootstrap, fx.Provide(peermgr.NewPeerMgr)),
		fx.Provide(
			// Consensus settings
			modules.BuiltinDrandConfig,
			modules.UpgradeSchedule,
			modules.NetworkName,

			modules.SetGenesis,
			modules.RandomSchedule,

			// Network bootstrap
			modules.BuiltinBootstrap,
			modules.DrandBootstrap,

			// Consensus: LegacyVM
			vm.Syscalls,

			// Consensus: Chain storage/access
			chain.LoadGenesis,
			chain.NewBadBlockCache,
			modules.ChainStore,
			modules.StateManager,
			modules.ChainBitswap,
			modules.ChainBlockService, // todo: unused

			// Consensus: Chain sync
			modules.NewSyncer,
			exchange.NewClient,

			// Chain networking
			hello.NewHelloService,
			exchange.NewServer,

			// Chain mining API dependencies
			modules.NewSlashFilter,

			// Service: Message Pool
			modules.NewDefaultMaxFeeFunc,
			modules.MessagePool,

			// Service: Wallet
			fx.Annotate(messagesigner.NewMessageSigner, fx.As(new(messagesigner.MsgSigner))),
			// func(ms *messagesigner.MessageSigner) messagesigner.MsgSigner { return ms },
			wallet.NewWallet,
			fx.Annotate(
				wallet.NewWallet,
				fx.As(new(wallet.Default)),
			),
			func(multiWallet wallet.MultiWallet) api.Wallet {
				return &multiWallet
			},

			// Service: Payment channels
			func(in modules.PaychAPI) paychmgr.PaychAPI {
				return &in
			},
			modules.NewPaychStore,
			modules.NewManager,

			// Markets (common)
			modules.NewLocalDiscovery,

			// Markets (retrieval)
			modules.RetrievalResolver,
			modules.RetrievalBlockstoreAccessor,
			modules.NewClientGraphsyncDataTransfer,

			// Markets (storage)
			market.NewFundManager,
			modules.NewClientDatastore,
			modules.StorageBlockstoreAccessor,
			modules.StorageClient,
			storageadapter.NewClientNodeAdapter,
			retrievaladapter.NewAPIBlockstoreAdapter,
			full.NewGasPriceCache,
		),

		// Defaults
		fx.Provide(
			journal.EnvDisabledEvents,
			modules.OpenFilesystemJournal,
			alerting.NewAlertingSystem,
			func(lc fx.Lifecycle, mctx helpers.MetricsCtx) context.Context {
				return helpers.LifecycleCtx(mctx, lc)
			},
		),
		fx.Supply(
			dtypes.NodeStartTime(time.Now()),
			fx.Annotate(
				metricsi.CtxScope(context.Background(), "lotus"),
				fx.As(new(helpers.MetricsCtx)),
			),
		),
	)
}

// Providers exclusive to full node
var fullNodeAPIProviders = fx.Provide(
	messagepool.NewProvider,
	fx.Annotate(modules.MessagePool, fx.As(new(messagepool.MpoolNonceAPI))),
	fx.Annotate(stmgr.NewStateManager, fx.As(new(stmgr.StateManagerAPI))),
	func(
		chainModule full.ChainModule,
		gasModule full.GasModule,
		mpoolModule full.MpoolModule,
		stateModule full.StateModule,
	) (full.ChainModuleAPI, full.GasModuleAPI, full.MpoolModuleAPI, full.StateModuleAPI) {
		return &chainModule, &gasModule, &mpoolModule, &stateModule
	},
)

// Providers exclusive to lite node
var liteNodeAPIProviders = fx.Provide(
	messagepool.NewProviderLite,
	fx.Annotate(rpcstmgr.NewRPCStateManager, fx.As(new(stmgr.StateManagerAPI))),
	func(nonceAPI modules.MpoolNonceAPI) messagepool.MpoolNonceAPI {
		return &nonceAPI
	},
	func(gateway api.Gateway) (
		full.ChainModuleAPI, full.GasModuleAPI, full.MpoolModuleAPI, full.StateModuleAPI /*full.EthModuleAPI,*/, full.EthEventAPI) {
		return gateway, gateway, gateway, gateway /*gateway,*/, gateway
	},
)
