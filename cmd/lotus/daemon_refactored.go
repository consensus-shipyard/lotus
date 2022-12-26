package main

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-paramfetch"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/ulimit"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/impl"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/lp2p"
	"github.com/filecoin-project/lotus/node/modules/testing"
	"github.com/filecoin-project/lotus/node/repo"
	metricsprom "github.com/ipfs/go-metrics-prometheus"
	"github.com/mitchellh/go-homedir"
	"github.com/multiformats/go-multiaddr"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/plugin/runmetrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
	"net/http"
	"os"
	"runtime/pprof"
)

var RefactoredDaemonCmd = func() *cli.Command {
	cmd := *DaemonCmd
	cmd.Name = "ddd"
	cmd.Usage = "Refactored daemon command"
	cmd.Action = refactoredDaemonAction
	return &cmd
}()

func refactoredDaemonAction(cctx *cli.Context) error {
	isLite := cctx.Bool("lite")
	isMirValidator := cctx.Bool("mir-validator")
	log.Warnf("mir-validator = %v", isMirValidator)

	err := runmetrics.Enable(runmetrics.RunMetricOptions{
		EnableCPU:    true,
		EnableMemory: true,
	})
	if err != nil {
		return xerrors.Errorf("enabling runtime metrics: %w", err)
	}

	if cctx.Bool("manage-fdlimit") {
		if _, _, err := ulimit.ManageFdLimit(); err != nil {
			log.Errorf("setting file descriptor limit: %s", err)
		}
	}

	if prof := cctx.String("pprof"); prof != "" {
		profile, err := os.Create(prof)
		if err != nil {
			return err
		}

		if err := pprof.StartCPUProfile(profile); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	var isBootstrapper dtypes.Bootstrapper
	switch profile := cctx.String("profile"); profile {
	case "bootstrapper":
		isBootstrapper = true
	case "":
		// do nothing
	default:
		return fmt.Errorf("unrecognized profile type: %q", profile)
	}

	ctx, _ := tag.New(context.Background(),
		tag.Insert(metrics.Version, build.BuildVersion),
		tag.Insert(metrics.Commit, build.CurrentCommit),
		tag.Insert(metrics.NodeType, "chain"),
	)
	// Register all metric views
	if err = view.Register(
		metrics.ChainNodeViews...,
	); err != nil {
		log.Fatalf("Cannot register the view: %v", err)
	}
	// Set the metric to one so it is published to the exporter
	stats.Record(ctx, metrics.LotusInfo.M(1))

	{
		dir, err := homedir.Expand(cctx.String("repo"))
		if err != nil {
			log.Warnw("could not expand repo location", "error", err)
		} else {
			log.Infof("lotus repo: %s", dir)
		}
	}

	r, err := repo.NewFS(cctx.String("repo"))
	if err != nil {
		return xerrors.Errorf("opening fs repo: %w", err)
	}

	if cctx.String("config") != "" {
		r.SetConfigPath(cctx.String("config"))
	}

	err = r.Init(repo.FullNode)
	if err != nil && err != repo.ErrRepoExists {
		return xerrors.Errorf("repo init error: %w", err)
	}
	freshRepo := err != repo.ErrRepoExists

	if !isLite {
		if err := paramfetch.GetParams(lcli.ReqContext(cctx), build.ParametersJSON(), build.SrsJSON(), 0); err != nil {
			return xerrors.Errorf("fetching proof parameters: %w", err)
		}
	}

	var genBytes []byte
	if cctx.String("genesis") != "" {
		genBytes, err = os.ReadFile(cctx.String("genesis"))
		if err != nil {
			return xerrors.Errorf("reading genesis: %w", err)
		}
	} else {
		genBytes = build.MaybeGenesis()
	}

	if cctx.IsSet("restore") {
		if !freshRepo {
			return xerrors.Errorf("restoring from backup is only possible with a fresh repo!")
		}
		if err := restore(cctx, r); err != nil {
			return xerrors.Errorf("restoring from backup: %w", err)
		}
	}

	chainfile := cctx.String("import-chain")
	snapshot := cctx.String("import-snapshot")
	if chainfile != "" || snapshot != "" {
		if chainfile != "" && snapshot != "" {
			return fmt.Errorf("cannot specify both 'import-snapshot' and 'import-chain'")
		}
		var issnapshot bool
		if chainfile == "" {
			chainfile = snapshot
			issnapshot = true
		}

		if err := ImportChain(ctx, r, chainfile, issnapshot); err != nil {
			return err
		}
		if cctx.Bool("halt-after-import") {
			fmt.Println("Chain import complete, halting as requested...")
			return nil
		}
	}

	genesis := fx.Options()
	if len(genBytes) > 0 {
		genesis = fx.Provide(modules.LoadGenesis(genBytes))
	}
	if cctx.String(makeGenFlag) != "" {
		if cctx.String(preTemplateFlag) == "" {
			return xerrors.Errorf("must also pass file with genesis template to `--%s`", preTemplateFlag)
		}
		genesis = fx.Provide(testing.MakeGenesis(cctx.String(makeGenFlag), cctx.String(preTemplateFlag)))
	}

	shutdownChan := make(chan struct{})

	// If the daemon is started in "lite mode", provide a  Gateway
	// for RPC calls
	liteModeDeps := fx.Options()
	if isLite {
		gapi, closer, err := lcli.GetGatewayAPI(cctx)
		if err != nil {
			return err
		}

		defer closer()
		liteModeDeps = fx.Supply(gapi)
	}

	// some libraries like ipfs/go-ds-measure and ipfs/go-ipfs-blockstore
	// use ipfs/go-metrics-interface. This injects a Prometheus exporter
	// for those. Metrics are exported to the default registry.
	if err := metricsprom.Inject(); err != nil {
		log.Warnf("unable to inject prometheus ipfs/go-metrics exporter; some metrics will be unavailable; err: %s", err)
	}

	//////////////////////////////////////////
	//// BEGIN REFACTORED NODE CONSTRUCTION //
	//////////////////////////////////////////
	fxProviders := make([]fx.Option, 0)
	fxInvokes := make([]fx.Option, 28)
	for i := range fxInvokes {
		fxInvokes[i] = fx.Options()
	}

	// repo setup
	lockedRepo, err := r.Lock(repo.FullNode)
	if err != nil {
		panic(err)
	}
	c, err := lockedRepo.Config()
	if err != nil {
		panic(err)
	}
	cfg, ok := c.(*config.FullNode)
	if !ok {
		panic("invalid config from repo")
	}

	// Refactored into modules
	var consensusModule fx.Option
	if build.IsMirConsensus() {
		consensusModule = mir.Module
	} else {
		consensusModule = fx.Provide(
			fx.Provide(filcns.NewFilecoinExpectedConsensus),
			fx.Supply(store.WeightFunc(filcns.Weight)),
			fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(filcns.RewardFunc), fx.As(new(stmgr.Executor)))),
		)
	}

	fxProviders = append(fxProviders,
		lp2p.Module(&cfg.Common),
		repoModule(lockedRepo),
		consensusModule,
		// orphaned, but likely belongs in some repo/store module
		fx.Provide(modules.Datastore(cfg.Backup.DisableMetadataLog)),
	)

	// node.defaults
	fxProviders = append(fxProviders, node.FxDefaultsProviders)
	mergeInvokes(fxInvokes, node.FxDefaultsInvokers())

	// node.Base()
	fxProviders = append(
		fxProviders,
		//node.FxLibP2PProviders,
		node.FxChainNodeProviders)
	//mergeInvokes(fxInvokes, node.FxLibP2PInvokers())
	mergeInvokes(fxInvokes, node.FxChainNodeInvokers())

	// node.Repo()
	//// providers and invokes
	fxProviders = append(
		fxProviders,
		//node.FxRepoProviders(lockedRepo, cfg),
		node.FxConfigFullNodeProviders(cfg),
	)
	mergeInvokes(fxInvokes, node.FxConfigCommonInvokers(&cfg.Common))

	// misc providers
	fxProviders = append(
		fxProviders,
		fx.Supply(isBootstrapper),
		fx.Supply(dtypes.ShutdownChan(shutdownChan)),
		genesis,
		liteModeDeps,
	)

	//TODO(hmz): leave these for later in the refactoring
	//node.ApplyIf(func(s *node.Settings) bool { return cctx.IsSet("api") },
	//	node.Override(node.SetApiEndpointKey, func(lr repo.LockedRepo) error {
	//		apima, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/" +
	//			cctx.String("api"))
	//		if err != nil {
	//			return err
	//		}
	//		return lr.SetAPIEndpoint(apima)
	//	})),
	//node.ApplyIf(func(s *node.Settings) bool { return !cctx.Bool("bootstrap") },
	//	node.Unset(node.RunPeerMgrKey),
	//	node.Unset(new(*peermgr.PeerMgr)),
	//),

	// Debugging of the dependency graph
	fxInvokes = append(fxInvokes,
		fx.Invoke(
			func(dotGraph fx.DotGraph) {
				os.WriteFile("fx.dot", []byte(dotGraph), 0660)
			}),
	)

	fxProviders = append(fxProviders, startupModule(cctx, r, lockedRepo, cfg))

	var rpcStopper node.StopFunc
	app := fx.New(
		fx.Options(fxProviders...),
		fx.Options(fxInvokes...),
		fx.Populate(&rpcStopper),
	)

	// TODO: we probably should have a 'firewall' for Closing signal
	//  on this context, and implement closing logic through lifecycles
	//  correctly
	if err := app.Start(ctx); err != nil {
		return xerrors.Errorf("starting node: %w", err)
	}
	////////////////////////////////////////
	//// END REFACTORED NODE CONSTRUCTION //
	////////////////////////////////////////

	//if cctx.String("import-key") != "" {
	//	if err := importKey(ctx, api, cctx.String("import-key")); err != nil {
	//		log.Errorf("importing key failed: %+v", err)
	//	}
	//}

	// Monitor for shutdown.
	finishCh := node.MonitorShutdown(shutdownChan,
		node.ShutdownHandler{Component: "rpc server", StopFunc: rpcStopper},
		node.ShutdownHandler{Component: "node", StopFunc: app.Stop},
	)
	<-finishCh // fires when shutdown is complete.

	// TODO: properly parse api endpoint (or make it a URL)
	return nil
}

var repoModule = func(lr repo.LockedRepo) fx.Option {
	return fx.Module("repo",
		fx.Provide(
			modules.LockedRepo(lr),
			modules.KeyStore,
			modules.APISecret,
		),
	)
}

func startupModule(cctx *cli.Context, r *repo.FsRepo, lr repo.LockedRepo, cfg *config.FullNode) fx.Option {
	return fx.Module("startup",
		fx.Provide(newFullNodeHandler),
		fx.Supply(newServerOptions(cctx.Int("api-max-req-size"))),
		fx.Supply(r),
		fx.Supply(lr),
		fx.Provide(
			func(api impl.FullNodeAPI, lr repo.LockedRepo, e dtypes.APIEndpoint) lapi.FullNode {
				lr.SetAPIEndpoint(e)
				return &api
			},
		),
		fx.Provide(
			func() (dtypes.APIEndpoint, error) {
				return multiaddr.NewMultiaddr(cfg.API.ListenAddress)
			},
		),
		fx.Provide(startRPCServer),
		//func(lr repo.LockedRepo, e dtypes.APIEndpoint) error {
		//	return lr.SetAPIEndpoint(e)
		//},
	)
}

func newServerOptions(maxRequestSize int) []jsonrpc.ServerOption {
	serverOptions := []jsonrpc.ServerOption{jsonrpc.WithServerErrors(lapi.RPCErrors)}
	if maxRequestSize > 0 {
		serverOptions = append(serverOptions, jsonrpc.WithMaxRequestSize(int64(maxRequestSize)))
	}
	return serverOptions
}

func newFullNodeHandler(api lapi.FullNode, serverOptions []jsonrpc.ServerOption) (http.Handler, error) {
	return node.FullNodeHandler(api, true, serverOptions...)
}

func startRPCServer(h http.Handler, repo *repo.FsRepo) (node.StopFunc, error) {
	endpoint, err := repo.APIEndpoint()
	if err != nil {
		return nil, xerrors.Errorf("getting api endpoint: %w", err)
	}
	return node.ServeRPC(h, "lotus-daemon", endpoint)
}

func mergeInvokes(into []fx.Option, from []fx.Option) {
	if len(into) != len(from) {
		panic("failed to merge invokes due to different lengths")
	}

	for i, invoke := range from {
		if invoke != nil {
			into[i] = invoke
		}
	}
}
