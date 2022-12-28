package fxmodules

import (
	"context"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/exchange"
	"github.com/filecoin-project/lotus/chain/market"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/messagesigner"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/journal/alerting"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/markets/storageadapter"
	"github.com/filecoin-project/lotus/node/hello"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/helpers"
	"github.com/filecoin-project/lotus/paychmgr"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	metricsi "github.com/ipfs/go-metrics-interface"
	"github.com/urfave/cli/v2"
	"go.uber.org/fx"
	"time"
)

func Fullnode(cctx *cli.Context) fx.Option {
	return fx.Module("fullnode",
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
		optionalProvide(peermgr.NewPeerMgr, cctx.Bool("bootstrap")),
		fx.Provide(
			// Consensus settings
			modules.BuiltinDrandConfig,
			modules.UpgradeSchedule,
			modules.NetworkName,
			// this one is only checking if genesis has already been provided
			//fx.Provide(fx.ErrorGenesis),
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
			messagesigner.NewMessageSigner,
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
			// already provided later
			//fx.Provide(fx.RetrievalClient(false)),
			modules.NewClientGraphsyncDataTransfer,

			// Markets (storage)
			market.NewFundManager,
			modules.NewClientDatastore,
			modules.StorageBlockstoreAccessor,
			modules.StorageClient,
			storageadapter.NewClientNodeAdapter,

			full.NewGasPriceCache,

			// TODO: do this later
			// Lite node API
			//ApplyIf(isLiteNode,
			//	Override(new(messagepool.Provider), messagepool.NewProviderLite),
			//	Override(new(messagesigner.MpoolNonceAPI), From(new(fx.MpoolNonceAPI))),
			//	Override(new(full.ChainModuleAPI), From(new(api.Gateway))),
			//	Override(new(full.GasModuleAPI), From(new(api.Gateway))),
			//	Override(new(full.MpoolModuleAPI), From(new(api.Gateway))),
			//	Override(new(full.StateModuleAPI), From(new(api.Gateway))),
			//	Override(new(stmgr.StateManagerAPI), rpcstmgr.NewRPCStateManager),
			//),

			// Full node API / service startup
			//ApplyIf(isFullNode,
			messagepool.NewProvider,
			fx.Annotate(
				modules.MessagePool,
				fx.As(new(messagesigner.MpoolNonceAPI)),
			),
			func(in full.ChainModule) full.ChainModuleAPI {
				return &in
			},
			func(in full.GasModule) full.GasModuleAPI {
				return &in
			},
			func(in full.MpoolModule) full.MpoolModuleAPI {
				return &in
			},
			func(in full.StateModule) full.StateModuleAPI {
				return &in
			},
			fx.Annotate(
				stmgr.NewStateManager,
				fx.As(new(stmgr.StateManagerAPI)),
			),
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
