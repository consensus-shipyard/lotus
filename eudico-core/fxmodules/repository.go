package fxmodules

import (
	"go.uber.org/fx"

	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/repo"
)

func Repository(lr repo.LockedRepo, cfg *config.FullNode) fx.Option {
	return fx.Module("repo",
		fx.Provide(
			modules.LockedRepo(lr),
			modules.KeyStore,
			modules.APISecret,
			modules.Datastore(cfg.Backup.DisableMetadataLog),
		),
	)
}
