package fxmodules

import (
	"go.uber.org/fx"

	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

func Blockstore(cfg *config.FullNode) fx.Option {
	return fx.Module("blockstore",
		fx.Provide(
			modules.UniversalBlockstore,

			// TODO(hmoniz): assuming cfg.Chainstore.EnableSplitstore == false
			// check if this should always be the case for Eudico
			fx.Annotate(modules.ChainFlatBlockstore, fx.As(new(dtypes.BasicChainBlockstore))),
			fx.Annotate(modules.StateFlatBlockstore, fx.As(new(dtypes.BasicStateBlockstore))),
			func(blockstore dtypes.UniversalBlockstore) dtypes.BaseBlockstore {
				return (dtypes.BaseBlockstore)(blockstore)
			},
			func(blockstore dtypes.UniversalBlockstore) dtypes.ExposedBlockstore {
				return (dtypes.ExposedBlockstore)(blockstore)
			},
			modules.NoopGCReferenceProtector,

			func(blockstore dtypes.BasicChainBlockstore) dtypes.ChainBlockstore {
				return (dtypes.ChainBlockstore)(blockstore)
			},
			func(blockstore dtypes.BasicStateBlockstore) dtypes.StateBlockstore {
				return (dtypes.StateBlockstore)(blockstore)
			},

			modules.ClientImportMgr,

			modules.ClientBlockstore,

			modules.Graphsync(cfg.Client.SimultaneousTransfersForStorage, cfg.Client.SimultaneousTransfersForRetrieval),

			modules.RetrievalClient(cfg.Client.OffChainRetrieval),

			// then assuming that cfg.Client.UseIpfs, cfg.Wallet.RemoteBackend, cfg.Wallet.EnableLedger,
			// cfg.Wallet.DisableLocal are all false
			// TODO(hmoniz): fix so we don't depend on this assumption
		),
	)
}
