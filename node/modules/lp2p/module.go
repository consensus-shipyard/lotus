package lp2p

import (
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	ci "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/fx"
	"time"
)

func Module(cfg *config.Common) fx.Option {
	// private providers
	privateProviders := fx.Options(
		fx.Provide(
			fx.Private,
			// Host dependencies
			Peerstore,

			// Host settings
			DefaultTransports,
			SmuxTransport(),
			NoRelay(),
			Security(true, false),

			// Host
			fx.Annotate(PrivKey, fx.As(new(ci.PrivKey))),
			func(privKey ci.PrivKey) ci.PubKey {
				return privKey.GetPublic()
			},
			peer.IDFromPublicKey,

			// Routing
			modules.RecordValidator,

			// Services
			AutoNATService,

			// Services (connection management)
			// already provided later
			//fx.Provide(lp2p.ConnectionManager(50, 200, 20*time.Second, nil)),
			ConnGaterOption,

			// Services (resource management)
			ResourceManagerOption,

			// Services (connection management)
			ConnectionManager(
				cfg.Libp2p.ConnMgrLow,
				cfg.Libp2p.ConnMgrHigh,
				time.Duration(cfg.Libp2p.ConnMgrGrace),
				cfg.Libp2p.ProtectedPeers),
			AddrsFactory(cfg.Libp2p.AnnounceAddresses, cfg.Libp2p.NoAnnounceAddresses),
		),
		fx.Supply(
			fx.Private,
			&cfg.Pubsub,
		),
	)

	if !cfg.Libp2p.DisableNatPortMap {
		privateProviders = fx.Options(privateProviders, fx.Provide(NatPortMap))
	}

	// public providers
	publicProviders := fx.Options(
		fx.Provide(
			// Host
			Host,
			RoutedHost,
			DHTRouting(dht.ModeAuto),

			//DiscoveryHandler, -- has no dependents

			// Routing
			BaseRouting,
			Routing,

			// Services
			BandwidthCounter,

			// Services (pubsub)
			ScoreKeeper,
			// these are already provided later
			//fx.Provide(lp2p.GossipSub),
			//fx.Provide(func(bs dtypes.Bootstrapper) *config.Pubsub {
			//	return &config.Pubsub{
			//		Bootstrapper: bool(bs),
			//	}
			//}),

			// Services (connection management)
			ConnGater,
			GossipSub,

			// Services (resource management)
			ResourceManager(cfg.Libp2p.ConnMgrHigh),
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
			PstoreAddSelfKeys,
			StartListening(config.DefaultFullNode().Libp2p.ListenAddresses),
		),
	)
}
