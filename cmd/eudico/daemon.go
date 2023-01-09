//go:build !nodaemon
// +build !nodaemon

package main

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-paramfetch"
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/eudico/fxmodules"
	"github.com/filecoin-project/lotus/eudico/global"
	"github.com/filecoin-project/lotus/lib/ulimit"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/config"
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

const (
	makeGenFlag     = "eudico-make-genesis"
	preTemplateFlag = "genesis-template"
)

var daemonStopCmd = &cli.Command{
	Name:  "stop",
	Usage: "Stop a running lotus daemon",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		err = api.Shutdown(lcli.ReqContext(cctx))
		if err != nil {
			return err
		}

		return nil
	},
}

// DaemonCmd is the `eudico daemon` command
func daemonCmd(consensusAlgorithm global.ConsensusAlgorithm) *cli.Command {
	return &cli.Command{
		Name:  "daemon",
		Usage: "Start a eudico daemon process",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "api",
				Value: "1234",
			},
			&cli.StringFlag{
				Name:   makeGenFlag,
				Value:  "",
				Hidden: true,
			},
			&cli.StringFlag{
				Name:   preTemplateFlag,
				Hidden: true,
			},
			&cli.StringFlag{
				Name:   "import-key",
				Usage:  "on first run, import a default key from a given file",
				Hidden: false,
			},
			&cli.StringFlag{
				Name:  "genesis",
				Usage: "genesis file to use for first node run",
			},
			&cli.BoolFlag{
				Name:  "bootstrap",
				Value: false,
			},
			&cli.StringFlag{
				Name:  "import-chain",
				Usage: "on first run, load chain from given file or url and validate",
			},
			&cli.StringFlag{
				Name:  "import-snapshot",
				Usage: "import chain state from a given chain export file or url",
			},
			&cli.BoolFlag{
				Name:  "halt-after-import",
				Usage: "halt the process after importing chain from file",
			},
			&cli.StringFlag{
				Name:  "pprof",
				Usage: "specify name of file for writing cpu profile to",
			},
			&cli.StringFlag{
				Name:  "profile",
				Usage: "specify type of node",
			},
			&cli.BoolFlag{
				Name:  "manage-fdlimit",
				Usage: "manage open file limit",
				Value: true,
			},
			&cli.StringFlag{
				Name:  "config",
				Usage: "specify path of config file to use",
			},
			// FIXME: This is not the correct place to put this configuration
			//  option. Ideally it would be part of `config.toml` but at the
			//  moment that only applies to the node configuration and not outside
			//  components like the RPC server.
			&cli.IntFlag{
				Name:  "api-max-req-size",
				Usage: "maximum API request size accepted by the JSON RPC server",
			},
			&cli.PathFlag{
				Name:  "restore-config",
				Usage: "config file to use when restoring from backup",
			},
		},
		Action: EudicoDaemonAction(consensusAlgorithm),
		Subcommands: []*cli.Command{
			daemonStopCmd,
		},
	}
}

func EudicoDaemonAction(consensusAlgorithm global.ConsensusAlgorithm) func(*cli.Context) error {
	return func(cctx *cli.Context) error {
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

		chainfile := cctx.String("import-chain")
		snapshot := cctx.String("import-snapshot")
		if chainfile != "" || snapshot != "" {
			panic("import chain or snapshot is not supported")
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
			fxmodules.Consensus(consensusAlgorithm),
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
}
