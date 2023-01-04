package fxmodules

import (
	"go.uber.org/fx"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/lp2p"
	"github.com/filecoin-project/lotus/paychmgr/settler"
)

func Invokes(cfg *config.Common, isBootstrap bool, isMirValidator bool) fx.Option {
	return fx.Module("invokes",
		fx.Invoke(
			modules.MemoryWatchdog,                          // 1 defaults
			modules.CheckFdLimit(build.DefaultFDLimit),      // 2 defaults
			lp2p.PstoreAddSelfKeys,                          // 3 libp2p
			lp2p.StartListening(cfg.Libp2p.ListenAddresses), // 4 common config
			modules.DoSetGenesis,                            // 6
			modules.RunHello,                                // 7
			modules.RunChainExchange,                        // 8
			modules.HandleIncomingMessages,                  // 12
			modules.HandleMigrateClientFunds,                // 13
			modules.HandlePaychManager,                      // 14
			modules.RelayIndexerMessages,                    // 15
			settler.SettlePaymentChannels,                   // 24
		),
		optionalInvoke(modules.RunPeerMgr, isBootstrap),               // 10
		optionalInvoke(modules.HandleIncomingBlocks, !isMirValidator), // 11
	)
}
