package node

import (
	"github.com/filecoin-project/go-fil-markets/discovery"
	discoveryimpl "github.com/filecoin-project/go-fil-markets/discovery/impl"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/gen/slashfilter"
	rpcstmgr "github.com/filecoin-project/lotus/chain/stmgr/rpc"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/node/hello"
	"github.com/filecoin-project/lotus/paychmgr"
	"github.com/filecoin-project/lotus/paychmgr/settler"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"os"

	"go.uber.org/fx"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-fil-markets/retrievalmarket"
	"github.com/filecoin-project/go-fil-markets/storagemarket"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/exchange"
	"github.com/filecoin-project/lotus/chain/market"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/messagesigner"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
	ledgerwallet "github.com/filecoin-project/lotus/chain/wallet/ledger"
	"github.com/filecoin-project/lotus/chain/wallet/remotewallet"
	"github.com/filecoin-project/lotus/markets/storageadapter"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/impl/full"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/repo"
)

var FxChainNodeProviders = fx.Options(
	// Consensus settings
	fx.Provide(modules.BuiltinDrandConfig),
	fx.Provide(modules.UpgradeSchedule),
	fx.Provide(modules.NetworkName),
	// this one is only checking if genesis has already been provided
	//fx.Provide(fx.ErrorGenesis),
	fx.Provide(modules.SetGenesis),
	fx.Provide(modules.RandomSchedule),

	// Network bootstrap
	fx.Provide(modules.BuiltinBootstrap),
	fx.Provide(modules.DrandBootstrap),

	// Consensus: crypto dependencies
	fx.Supply(
		fx.Annotate(
			ffiwrapper.ProofVerifier,
			fx.As(new(storiface.Verifier)),
		),
	),
	fx.Supply(
		fx.Annotate(
			ffiwrapper.ProofProver,
			fx.As(new(storiface.Prover)),
		),
	),

	// Consensus: LegacyVM
	fx.Provide(vm.Syscalls),

	// Consensus: Chain storage/access
	fx.Provide(chain.LoadGenesis),
	//fx.Supply(filcns.Weight),
	fx.Provide(chain.NewBadBlockCache),
	fx.Provide(modules.ChainStore),
	fx.Provide(modules.StateManager),
	fx.Provide(modules.ChainBitswap),
	fx.Provide(modules.ChainBlockService), // todo: unused

	// Consensus: Chain sync

	// We don't want the SyncManagerCtor to be used as an fx constructor, but rather as a value.
	// It will be called implicitly by the Syncer constructor.
	fx.Supply(chain.SyncManagerCtor(chain.NewSyncManager)),
	fx.Provide(modules.NewSyncer),
	fx.Provide(exchange.NewClient),

	// Chain networking
	fx.Provide(hello.NewHelloService),
	fx.Provide(exchange.NewServer),
	fx.Provide(peermgr.NewPeerMgr),

	// Chain mining API dependencies
	fx.Provide(modules.NewSlashFilter),

	// Service: Message Pool
	fx.Provide(modules.NewDefaultMaxFeeFunc),
	fx.Provide(modules.MessagePool),
	fx.Supply(new(dtypes.MpoolLocker)),

	// Shared graphsync (markets, serving chain)
	// already provided later
	//fx.Provide(fx.Graphsync(config.DefaultFullNode().Client.SimultaneousTransfersForStorage, config.DefaultFullNode().Client.SimultaneousTransfersForRetrieval)),

	// Service: Wallet
	fx.Provide(messagesigner.NewMessageSigner),
	fx.Provide(wallet.NewWallet),
	fx.Provide(
		fx.Annotate(
			wallet.NewWallet,
			fx.As(new(wallet.Default)),
		),
	),
	fx.Provide(
		func(multiWallet wallet.MultiWallet) api.Wallet {
			return &multiWallet
		},
	),

	// Service: Payment channels
	fx.Provide(
		func(in modules.PaychAPI) paychmgr.PaychAPI {
			return &in
		},
	),
	fx.Provide(modules.NewPaychStore),
	fx.Provide(modules.NewManager),

	// Markets (common)
	fx.Provide(modules.NewLocalDiscovery),

	// Markets (retrieval)
	fx.Provide(modules.RetrievalResolver),
	fx.Provide(modules.RetrievalBlockstoreAccessor),
	// already provided later
	//fx.Provide(fx.RetrievalClient(false)),
	fx.Provide(modules.NewClientGraphsyncDataTransfer),

	// Markets (storage)
	fx.Provide(market.NewFundManager),
	fx.Provide(modules.NewClientDatastore),
	fx.Provide(modules.StorageBlockstoreAccessor),
	fx.Provide(modules.StorageClient),
	fx.Provide(storageadapter.NewClientNodeAdapter),

	fx.Provide(full.NewGasPriceCache),

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
	fx.Provide(messagepool.NewProvider),
	fx.Provide(
		fx.Annotate(
			From(new(*messagepool.MessagePool)),
			fx.As(new(messagesigner.MpoolNonceAPI)),
		),
	),
	fx.Provide(
		func(in full.ChainModule) full.ChainModuleAPI {
			return &in
		},
	),
	fx.Provide(
		func(in full.GasModule) full.GasModuleAPI {
			return &in
		},
	),
	fx.Provide(
		func(in full.MpoolModule) full.MpoolModuleAPI {
			return &in
		},
	),
	fx.Provide(
		func(in full.StateModule) full.StateModuleAPI {
			return &in
		},
	),
	fx.Provide(
		fx.Annotate(
			From(new(*stmgr.StateManager)),
			fx.As(new(stmgr.StateManagerAPI)),
		),
	),

	// We're omitting Mir providers and invokers because they are a verbatim
	// copy of above (why???)
)

func FxChainNodeInvokers() []fx.Option {
	invokers := make([]fx.Option, _nInvokes)
	invokers[SetGenesisKey] = fx.Invoke(modules.DoSetGenesis)
	invokers[HandlePaymentChannelManagerKey] = fx.Invoke(modules.HandlePaychManager)
	invokers[SettlePaymentChannelsKey] = fx.Invoke(settler.SettlePaymentChannels)
	invokers[HandleMigrateClientFundsKey] = fx.Invoke(modules.HandleMigrateClientFunds)
	invokers[RelayIndexerMessagesKey] = fx.Invoke(modules.RelayIndexerMessages)
	invokers[RunHelloKey] = fx.Invoke(modules.RunHello)
	invokers[RunChainExchangeKey] = fx.Invoke(modules.RunChainExchange)
	invokers[RunPeerMgrKey] = fx.Invoke(modules.RunPeerMgr)
	invokers[HandleIncomingMessagesKey] = fx.Invoke(modules.HandleIncomingMessages)
	// TODO(hmz): this invoker shouldn't be created if MirValidator
	invokers[HandleIncomingBlocksKey] = fx.Invoke(modules.HandleIncomingBlocks)
	return invokers
}

// Chain node provides access to the Filecoin blockchain, by setting up a full
// validator node, or by delegating some actions to other nodes (lite mode)
var ChainNode = Options(
	// Full node or lite node
	// TODO: Fix offline mode

	// Consensus settings
	Override(new(dtypes.DrandSchedule), modules.BuiltinDrandConfig),
	Override(new(stmgr.UpgradeSchedule), modules.UpgradeSchedule),
	Override(new(dtypes.NetworkName), modules.NetworkName),
	Override(new(modules.Genesis), modules.ErrorGenesis),
	Override(new(dtypes.AfterGenesisSet), modules.SetGenesis),
	Override(SetGenesisKey, modules.DoSetGenesis),
	Override(new(beacon.Schedule), modules.RandomSchedule),

	// Network bootstrap
	Override(new(dtypes.BootstrapPeers), modules.BuiltinBootstrap),
	Override(new(dtypes.DrandBootstrap), modules.DrandBootstrap),

	// Consensus: crypto dependencies
	Override(new(storiface.Verifier), ffiwrapper.ProofVerifier),
	Override(new(storiface.Prover), ffiwrapper.ProofProver),

	// Consensus: LegacyVM
	Override(new(vm.SyscallBuilder), vm.Syscalls),

	// Consensus: Chain storage/access
	Override(new(chain.Genesis), chain.LoadGenesis),
	Override(new(store.WeightFunc), filcns.Weight),
	Override(new(stmgr.Executor), consensus.NewTipSetExecutor(filcns.RewardFunc)),
	Override(new(*chain.BadBlockCache), chain.NewBadBlockCache),
	Override(new(consensus.Consensus), filcns.NewFilecoinExpectedConsensus),
	Override(new(*store.ChainStore), modules.ChainStore),
	Override(new(*stmgr.StateManager), modules.StateManager),
	Override(new(dtypes.ChainBitswap), modules.ChainBitswap),
	Override(new(dtypes.ChainBlockService), modules.ChainBlockService), // todo: unused

	// Consensus: Chain sync

	// We don't want the SyncManagerCtor to be used as an fx constructor, but rather as a value.
	// It will be called implicitly by the Syncer constructor.
	Override(new(chain.SyncManagerCtor), func() chain.SyncManagerCtor { return chain.NewSyncManager }),
	Override(new(*chain.Syncer), modules.NewSyncer),
	Override(new(exchange.Client), exchange.NewClient),

	// Chain networking
	Override(new(*hello.Service), hello.NewHelloService),
	Override(new(exchange.Server), exchange.NewServer),
	Override(new(*peermgr.PeerMgr), peermgr.NewPeerMgr),

	// Chain networking
	Override(new(*hello.Service), hello.NewHelloService),
	Override(new(exchange.Server), exchange.NewServer),
	Override(new(*peermgr.PeerMgr), peermgr.NewPeerMgr),

	// Chain mining API dependencies
	Override(new(*slashfilter.SlashFilter), modules.NewSlashFilter),

	// Service: Message Pool
	Override(new(dtypes.DefaultMaxFeeFunc), modules.NewDefaultMaxFeeFunc),
	Override(new(*messagepool.MessagePool), modules.MessagePool),
	Override(new(*dtypes.MpoolLocker), new(dtypes.MpoolLocker)),

	// Shared graphsync (markets, serving chain)
	Override(new(dtypes.Graphsync), modules.Graphsync(config.DefaultFullNode().Client.SimultaneousTransfersForStorage, config.DefaultFullNode().Client.SimultaneousTransfersForRetrieval)),

	// Service: Wallet
	Override(new(*messagesigner.MessageSigner), messagesigner.NewMessageSigner),
	Override(new(*wallet.LocalWallet), wallet.NewWallet),
	Override(new(wallet.Default), From(new(*wallet.LocalWallet))),
	Override(new(api.Wallet), From(new(wallet.MultiWallet))),

	// Service: Payment channels
	//Override(new(paychmgr.PaychAPI), From(new(fx.PaychAPI))),
	Override(new(*paychmgr.Store), modules.NewPaychStore),
	Override(new(*paychmgr.Manager), modules.NewManager),
	Override(HandlePaymentChannelManagerKey, modules.HandlePaychManager),
	Override(SettlePaymentChannelsKey, settler.SettlePaymentChannels),

	// Markets (common)
	Override(new(*discoveryimpl.Local), modules.NewLocalDiscovery),

	// Markets (retrieval)
	Override(new(discovery.PeerResolver), modules.RetrievalResolver),
	Override(new(retrievalmarket.BlockstoreAccessor), modules.RetrievalBlockstoreAccessor),
	Override(new(retrievalmarket.RetrievalClient), modules.RetrievalClient(false)),
	Override(new(dtypes.ClientDataTransfer), modules.NewClientGraphsyncDataTransfer),

	// Markets (storage)
	Override(new(*market.FundManager), market.NewFundManager),
	Override(new(dtypes.ClientDatastore), modules.NewClientDatastore),
	Override(new(storagemarket.BlockstoreAccessor), modules.StorageBlockstoreAccessor),
	Override(new(storagemarket.StorageClient), modules.StorageClient),
	Override(new(storagemarket.StorageClientNode), storageadapter.NewClientNodeAdapter),
	Override(HandleMigrateClientFundsKey, modules.HandleMigrateClientFunds),

	Override(new(*full.GasPriceCache), full.NewGasPriceCache),

	Override(RelayIndexerMessagesKey, modules.RelayIndexerMessages),

	// Lite node API
	ApplyIf(isLiteNode,
		Override(new(messagepool.Provider), messagepool.NewProviderLite),
		Override(new(messagesigner.MpoolNonceAPI), From(new(modules.MpoolNonceAPI))),
		Override(new(full.ChainModuleAPI), From(new(api.Gateway))),
		Override(new(full.GasModuleAPI), From(new(api.Gateway))),
		Override(new(full.MpoolModuleAPI), From(new(api.Gateway))),
		Override(new(full.StateModuleAPI), From(new(api.Gateway))),
		Override(new(stmgr.StateManagerAPI), rpcstmgr.NewRPCStateManager),
	),

	// Full node API / service startup
	ApplyIf(isFullNode,
		Override(new(messagepool.Provider), messagepool.NewProvider),
		Override(new(messagesigner.MpoolNonceAPI), From(new(*messagepool.MessagePool))),
		Override(new(full.ChainModuleAPI), From(new(full.ChainModule))),
		Override(new(full.GasModuleAPI), From(new(full.GasModule))),
		Override(new(full.MpoolModuleAPI), From(new(full.MpoolModule))),
		Override(new(full.StateModuleAPI), From(new(full.StateModule))),
		Override(new(stmgr.StateManagerAPI), From(new(*stmgr.StateManager))),

		Override(RunHelloKey, modules.RunHello),
		Override(RunChainExchangeKey, modules.RunChainExchange),
		Override(RunPeerMgrKey, modules.RunPeerMgr),
		Override(HandleIncomingMessagesKey, modules.HandleIncomingMessages),
		Override(HandleIncomingBlocksKey, modules.HandleIncomingBlocks),
	),

	//// Mir validators don't handle incoming blocks through pubsub.
	//// all that is handled by Mir.
	ApplyIf(isMirvalidator,
		Override(new(messagepool.Provider), messagepool.NewProvider),
		Override(new(messagesigner.MpoolNonceAPI), From(new(*messagepool.MessagePool))),
		Override(new(full.ChainModuleAPI), From(new(full.ChainModule))),
		Override(new(full.GasModuleAPI), From(new(full.GasModule))),
		Override(new(full.MpoolModuleAPI), From(new(full.MpoolModule))),
		Override(new(full.StateModuleAPI), From(new(full.StateModule))),
		Override(new(stmgr.StateManagerAPI), From(new(*stmgr.StateManager))),

		Override(RunHelloKey, modules.RunHello),
		Override(RunChainExchangeKey, modules.RunChainExchange),
		Override(RunPeerMgrKey, modules.RunPeerMgr),
		Override(HandleIncomingMessagesKey, modules.HandleIncomingMessages),
		// Override(HandleIncomingBlocksKey, Modules.HandleIncomingBlocks),
	),
)

func FxConfigFullNodeProviders(cfg *config.FullNode) fx.Option {
	return fx.Options(
		//FxConfigCommonProviders(&cfg.Common),
		fx.Provide(modules.UniversalBlockstore),

		// TODO(hmz): assuming cfg.Chainstore.EnableSplitstore == false
		// check if this should always be the case for Eudico
		fx.Provide(fx.Annotate(modules.ChainFlatBlockstore, fx.As(new(dtypes.BasicChainBlockstore)))),
		fx.Provide(fx.Annotate(modules.StateFlatBlockstore, fx.As(new(dtypes.BasicStateBlockstore)))),
		fx.Provide(
			func(blockstore dtypes.UniversalBlockstore) dtypes.BaseBlockstore {
				return (dtypes.BaseBlockstore)(blockstore)
			},
		),
		fx.Provide(
			func(blockstore dtypes.UniversalBlockstore) dtypes.ExposedBlockstore {
				return (dtypes.ExposedBlockstore)(blockstore)
			},
		),
		fx.Provide(modules.NoopGCReferenceProtector),

		fx.Provide(
			func(blockstore dtypes.BasicChainBlockstore) dtypes.ChainBlockstore {
				return (dtypes.ChainBlockstore)(blockstore)
			},
		),
		fx.Provide(
			func(blockstore dtypes.BasicStateBlockstore) dtypes.StateBlockstore {
				return (dtypes.StateBlockstore)(blockstore)
			},
		),

		fx.Provide(modules.ClientImportMgr),

		fx.Provide(modules.ClientBlockstore),

		fx.Provide(modules.Graphsync(cfg.Client.SimultaneousTransfersForStorage, cfg.Client.SimultaneousTransfersForRetrieval)),

		fx.Provide(modules.RetrievalClient(cfg.Client.OffChainRetrieval)),

		// then assuming that cfg.Client.UseIpfs, cfg.Wallet.RemoteBackend, cfg.Wallet.EnableLedger,
		// cfg.Wallet.DisableLocal are all false
	)
}

func ConfigFullNode(c interface{}) Option {
	cfg, ok := c.(*config.FullNode)
	if !ok {
		return Error(xerrors.Errorf("invalid config from repo, got: %T", c))
	}

	enableLibp2pNode := true // always enable libp2p for full nodes

	ipfsMaddr := cfg.Client.IpfsMAddr
	return Options(
		ConfigCommon(&cfg.Common, enableLibp2pNode),

		Override(new(dtypes.UniversalBlockstore), modules.UniversalBlockstore),

		If(cfg.Chainstore.EnableSplitstore,
			If(cfg.Chainstore.Splitstore.ColdStoreType == "universal",
				Override(new(dtypes.ColdBlockstore), From(new(dtypes.UniversalBlockstore)))),
			If(cfg.Chainstore.Splitstore.ColdStoreType == "discard",
				Override(new(dtypes.ColdBlockstore), modules.DiscardColdBlockstore)),
			If(cfg.Chainstore.Splitstore.HotStoreType == "badger",
				Override(new(dtypes.HotBlockstore), modules.BadgerHotBlockstore)),
			Override(new(dtypes.SplitBlockstore), modules.SplitBlockstore(&cfg.Chainstore)),
			Override(new(dtypes.BasicChainBlockstore), modules.ChainSplitBlockstore),
			Override(new(dtypes.BasicStateBlockstore), modules.StateSplitBlockstore),
			Override(new(dtypes.BaseBlockstore), From(new(dtypes.SplitBlockstore))),
			Override(new(dtypes.ExposedBlockstore), modules.ExposedSplitBlockstore),
			Override(new(dtypes.GCReferenceProtector), modules.SplitBlockstoreGCReferenceProtector),
		),
		If(!cfg.Chainstore.EnableSplitstore,
			Override(new(dtypes.BasicChainBlockstore), modules.ChainFlatBlockstore),
			Override(new(dtypes.BasicStateBlockstore), modules.StateFlatBlockstore),
			Override(new(dtypes.BaseBlockstore), From(new(dtypes.UniversalBlockstore))),
			Override(new(dtypes.ExposedBlockstore), From(new(dtypes.UniversalBlockstore))),
			Override(new(dtypes.GCReferenceProtector), modules.NoopGCReferenceProtector),
		),

		Override(new(dtypes.ChainBlockstore), From(new(dtypes.BasicChainBlockstore))),
		Override(new(dtypes.StateBlockstore), From(new(dtypes.BasicStateBlockstore))),

		If(os.Getenv("LOTUS_ENABLE_CHAINSTORE_FALLBACK") == "1",
			Override(new(dtypes.ChainBlockstore), modules.FallbackChainBlockstore),
			Override(new(dtypes.StateBlockstore), modules.FallbackStateBlockstore),
			Override(SetupFallbackBlockstoresKey, modules.InitFallbackBlockstores),
		),

		Override(new(dtypes.ClientImportMgr), modules.ClientImportMgr),

		Override(new(dtypes.ClientBlockstore), modules.ClientBlockstore),

		If(cfg.Client.UseIpfs,
			Override(new(dtypes.ClientBlockstore), modules.IpfsClientBlockstore(ipfsMaddr, cfg.Client.IpfsOnlineMode)),
			Override(new(storagemarket.BlockstoreAccessor), modules.IpfsStorageBlockstoreAccessor),
			If(cfg.Client.IpfsUseForRetrieval,
				Override(new(retrievalmarket.BlockstoreAccessor), modules.IpfsRetrievalBlockstoreAccessor),
			),
		),
		Override(new(dtypes.Graphsync), modules.Graphsync(cfg.Client.SimultaneousTransfersForStorage, cfg.Client.SimultaneousTransfersForRetrieval)),

		Override(new(retrievalmarket.RetrievalClient), modules.RetrievalClient(cfg.Client.OffChainRetrieval)),

		If(cfg.Wallet.RemoteBackend != "",
			Override(new(*remotewallet.RemoteWallet), remotewallet.SetupRemoteWallet(cfg.Wallet.RemoteBackend)),
		),
		If(cfg.Wallet.EnableLedger,
			Override(new(*ledgerwallet.LedgerWallet), ledgerwallet.NewWallet),
		),
		If(cfg.Wallet.DisableLocal,
			Unset(new(*wallet.LocalWallet)),
			Override(new(wallet.Default), wallet.NilDefault),
		),
	)
}

type FullOption = Option

func Lite(enable bool) FullOption {
	return func(s *Settings) error {
		s.Lite = enable
		return nil
	}
}

func MirValidator(enable bool) FullOption {
	return func(s *Settings) error {
		s.MirValidator = enable
		return nil
	}
}

// hmz: Checking that impl.FullNodeAPI implements api.FullNode
var _ api.FullNode = (*impl.FullNodeAPI)(nil)

func FullAPI(out *api.FullNode, fopts ...FullOption) Option {
	return Options(
		func(s *Settings) error {
			s.NodeType = repo.FullNode
			s.EnableLibp2pNode = true
			return nil
		},
		Options(fopts...),
		func(s *Settings) error {
			resAPI := &impl.FullNodeAPI{}
			s.Invokes[ExtractApiKey] = fx.Populate(resAPI)
			*out = resAPI
			return nil
		},
	)
}
