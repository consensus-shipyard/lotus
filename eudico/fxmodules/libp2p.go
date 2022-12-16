package fxmodules

import (
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/lp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	ci "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/fx"
	"time"
)

func Libp2p(cfg *config.Common) fx.Option {
	// private providers
	privateProviders := fx.Options(
		fx.Provide(
			fx.Private,

			// Host settings
			lp2p.DefaultTransports,
			lp2p.SmuxTransport(),
			lp2p.NoRelay(),
			lp2p.Security(true, false),

			// Routing
			modules.RecordValidator,

			// Services
			lp2p.AutoNATService,

			// Services (connection management)
			// already provided later
			//fx.Provide(lp2p.ConnectionManager(50, 200, 20*time.Second, nil)),
			lp2p.ConnGaterOption,

			// Services (resource management)
			lp2p.ResourceManagerOption,

			// Services (connection management)
			lp2p.ConnectionManager(
				cfg.Libp2p.ConnMgrLow,
				cfg.Libp2p.ConnMgrHigh,
				time.Duration(cfg.Libp2p.ConnMgrGrace),
				cfg.Libp2p.ProtectedPeers),
			lp2p.AddrsFactory(cfg.Libp2p.AnnounceAddresses, cfg.Libp2p.NoAnnounceAddresses),
		),
		fx.Supply(
			fx.Private,
			&cfg.Pubsub,
		),
	)

	if !cfg.Libp2p.DisableNatPortMap {
		privateProviders = fx.Options(privateProviders, fx.Provide(lp2p.NatPortMap))
	}

	// public providers
	publicProviders := fx.Options(
		fx.Provide(
			// Host dependencies
			lp2p.Peerstore,

			// Host
			lp2p.Host,
			lp2p.RoutedHost,
			lp2p.DHTRouting(dht.ModeAuto),
			fx.Annotate(lp2p.PrivKey, fx.As(new(ci.PrivKey))),
			func(privKey ci.PrivKey) ci.PubKey {
				return privKey.GetPublic()
			},
			peer.IDFromPublicKey,

			//DiscoveryHandler, -- has no dependents

			// Routing
			lp2p.BaseRouting,
			lp2p.Routing,

			// Services
			lp2p.BandwidthCounter,

			// Services (pubsub)
			lp2p.ScoreKeeper,

			// Services (connection management)
			lp2p.ConnGater,
			lp2p.GossipSub,

			// Services (resource management)
			lp2p.ResourceManager(cfg.Libp2p.ConnMgrHigh),
		),
	)

	if len(cfg.Libp2p.BootstrapPeers) > 0 {
		publicProviders = fx.Options(
			publicProviders,
			fx.Provide(modules.ConfigBootstrap(cfg.Libp2p.BootstrapPeers)),
		)
	}

	return fx.Module("libp2p",
		privateProviders,
		publicProviders,
		fx.Invoke(
			lp2p.PstoreAddSelfKeys,
			lp2p.StartListening(config.DefaultFullNode().Libp2p.ListenAddresses),
		),
	)
}
