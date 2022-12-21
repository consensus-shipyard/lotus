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
	"github.com/filecoin-project/lotus/node/modules/testing"
	"github.com/filecoin-project/lotus/node/repo"
	metricsprom "github.com/ipfs/go-metrics-prometheus"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/plugin/runmetrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/fx"
	"golang.org/x/xerrors"
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

	/*
		// node.defaults() - originally called from node.New
		options := node.Defaults()

		// node.FullAPI()
		var api lapi.FullNode
		options = append(
			options,
			node.FullAPI(&api, node.Lite(false), node.MirValidator(false)),
		)

		fxOptions, _ := node.ConvertToFxOptions(options...)
	*/
	// node.Base()
	// node.Repo()

	// dtypes.Bootstrapper
	// dtypes.ShutdownChan

	// genesis
	// liteModeDeps

	// Mir Consensus

	// SetApiEndpointKey

	// Unset node.RunPeerMgrKey and peermgr.PeerMgr is not bootstrap

	return legacyDaemonAction(cctx)
}

func legacyDaemonAction(cctx *cli.Context) error {
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

	// node.defaults
	fxProviders = append(fxProviders, node.FxDefaultsProviders)
	mergeInvokes(fxInvokes, node.FxDefaultsInvokers())

	// node.FullAPI (settings are set further below)
	var api lapi.FullNode
	var resAPI = &impl.FullNodeAPI{}
	fxInvokes[node.ExtractApiKey] = fx.Populate(resAPI)
	api = resAPI

	// node.Base()
	fxProviders = append(
		fxProviders,
		node.FxLibP2PProviders,
		node.FxChainNodeProviders)
	mergeInvokes(fxInvokes, node.FxLibP2PInvokers())
	mergeInvokes(fxInvokes, node.FxChainNodeInvokers())

	//// consensus conditional providers
	if build.IsMirConsensus() {
		fxProviders = append(
			fxProviders,
			fx.Provide(mir.NewConsensus),
			fx.Supply(store.WeightFunc(mir.Weight)),
			fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(mir.RewardFunc), fx.As(new(stmgr.Executor)))),
		)
	} else {
		fxProviders = append(
			fxProviders,
			fx.Provide(filcns.NewFilecoinExpectedConsensus),
			fx.Supply(store.WeightFunc(filcns.Weight)),
			fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(filcns.RewardFunc), fx.As(new(stmgr.Executor)))),
		)
	}

	// node.Repo()
	//// setup
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

	//// providers and invokes
	fxProviders = append(
		fxProviders,
		node.FxRepoProviders(lockedRepo, cfg))
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

	app := fx.New(fx.Options(fxProviders...), fx.Options(fxInvokes...))

	// TODO: we probably should have a 'firewall' for Closing signal
	//  on this context, and implement closing logic through lifecycles
	//  correctly
	if err := app.Start(ctx); err != nil {
		return xerrors.Errorf("starting node: %w", err)
	}
	////////////////////////////////////////
	//// END REFACTORED NODE CONSTRUCTION //
	////////////////////////////////////////

	if cctx.String("import-key") != "" {
		if err := importKey(ctx, api, cctx.String("import-key")); err != nil {
			log.Errorf("importing key failed: %+v", err)
		}
	}

	endpoint, err := r.APIEndpoint()
	if err != nil {
		return xerrors.Errorf("getting api endpoint: %w", err)
	}

	//
	// Instantiate JSON-RPC endpoint.
	// ----

	// Populate JSON-RPC options.
	serverOptions := []jsonrpc.ServerOption{jsonrpc.WithServerErrors(lapi.RPCErrors)}
	if maxRequestSize := cctx.Int("api-max-req-size"); maxRequestSize != 0 {
		serverOptions = append(serverOptions, jsonrpc.WithMaxRequestSize(int64(maxRequestSize)))
	}

	// Instantiate the full node handler.
	h, err := node.FullNodeHandler(api, true, serverOptions...)
	if err != nil {
		return fmt.Errorf("failed to instantiate rpc handler: %s", err)
	}

	// Serve the RPC.
	rpcStopper, err := node.ServeRPC(h, "lotus-daemon", endpoint)
	if err != nil {
		return fmt.Errorf("failed to start json-rpc endpoint: %s", err)
	}

	// Monitor for shutdown.
	finishCh := node.MonitorShutdown(shutdownChan,
		node.ShutdownHandler{Component: "rpc server", StopFunc: rpcStopper},
		node.ShutdownHandler{Component: "node", StopFunc: app.Stop},
	)
	<-finishCh // fires when shutdown is complete.

	// TODO: properly parse api endpoint (or make it a URL)
	return nil
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
