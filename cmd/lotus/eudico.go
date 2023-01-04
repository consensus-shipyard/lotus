package main

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"

	metricsprom "github.com/ipfs/go-metrics-prometheus"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/plugin/runmetrics"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/fx"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-paramfetch"

	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/eudico/fxmodules"
	"github.com/filecoin-project/lotus/lib/ulimit"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/modules/testing"
	"github.com/filecoin-project/lotus/node/repo"
)

var EudicoDaemonCmd = func() *cli.Command {
	cmd := *DaemonCmd
	cmd.Name = "eudico"
	cmd.Usage = "Eudico daemon command"
	cmd.Action = eudicoDaemonAction
	return &cmd
}()

func eudicoDaemonAction(cctx *cli.Context) error {
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

	fxProviders := fx.Options(
		fxmodules.Fullnode(cctx.Bool("bootstrap"), isLite),
		fxmodules.Libp2p(&cfg.Common),
		fxmodules.Repository(lockedRepo, cfg),
		fxmodules.Blockstore(cfg),
		fxmodules.Consensus(fxmodules.MirConsensus),
		fxmodules.RpcServer(cctx, r, lockedRepo, cfg),
		// misc providers
		fx.Supply(isBootstrapper),
		fx.Supply(dtypes.ShutdownChan(shutdownChan)),
		genesis,
		liteModeDeps,
	)

	var rpcStopper node.StopFunc
	app := fx.New(
		fxProviders,
		fx.Populate(&rpcStopper),
		fxmodules.Invokes(&cfg.Common, cctx.Bool("bootstrap"), isMirValidator),
		// Debugging of the dependency graph
		fx.Invoke(
			func(dotGraph fx.DotGraph) {
				os.WriteFile("fx.dot", []byte(dotGraph), 0660)
			}),
	)

	// TODO: we probably should have a 'firewall' for Closing signal
	//  on this context, and implement closing logic through lifecycles
	//  correctly
	if err := app.Start(ctx); err != nil {
		return xerrors.Errorf("starting node: %w", err)
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
