package fxmodules

import (
	"go.uber.org/fx"

	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

func Blockstore(cfg *config.FullNode) fx.Option {
	return fx.Module("blockstore",
		fxEitherOr(cfg.Chainstore.EnableSplitstore,
			fx.Options(
				fxCase(cfg.Chainstore.Splitstore.ColdStoreType,
					map[string]fx.Option{
						"universal": fx.Provide(fx.Annotate(modules.UniversalBlockstore, fx.As(new(dtypes.ColdBlockstore)))),
						"messages":  fx.Provide(fx.Annotate(modules.UniversalBlockstore, fx.As(new(dtypes.ColdBlockstore)))),
						"discard":   fx.Provide(modules.DiscardColdBlockstore),
					}),
				fxOptional(cfg.Chainstore.Splitstore.HotStoreType == "badger", fx.Provide(modules.BadgerHotBlockstore)),
				fx.Provide(
					modules.SplitBlockstore(&cfg.Chainstore),
					fx.Annotate(modules.ChainSplitBlockstore, fx.As(new(dtypes.BasicChainBlockstore))),
					modules.StateSplitBlockstore,
					modules.ExposedSplitBlockstore,
					modules.SplitBlockstoreGCReferenceProtector,
					func(blockstore dtypes.SplitBlockstore) dtypes.BaseBlockstore { return blockstore },
				),
			),
			fx.Provide(
				fx.Annotate(modules.ChainFlatBlockstore, fx.As(new(dtypes.BasicChainBlockstore))),
				modules.StateFlatBlockstore,
				func(blockstore dtypes.UniversalBlockstore) (dtypes.BaseBlockstore, dtypes.ExposedBlockstore) {
					return blockstore, blockstore
				},
				modules.NoopGCReferenceProtector,
			),
		),
		fx.Provide(
			modules.UniversalBlockstore,

			func(blockstore dtypes.BasicChainBlockstore) dtypes.ChainBlockstore {
				return blockstore
			},
			func(blockstore dtypes.BasicStateBlockstore) dtypes.StateBlockstore {
				return blockstore
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
