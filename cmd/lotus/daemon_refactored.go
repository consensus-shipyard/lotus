package main

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-paramfetch"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/peermgr"
	"github.com/filecoin-project/lotus/lib/ulimit"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
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

	// defaults() - called from node.New

	// node.FullAPI()
	// node.Base()
	// node.Repo()

	// dtypes.Bootstrapper
	// dtypes.ShutdownChan

	// genesis
	// liteModeDeps

	// Mir Consensus

	// SetApiEndpointKey

	// Unset node.RunPeerMgrKey and peermgr.PeerMgr is not bootstrap

	return legacyDaemonAction(cctx, fx.Options())
}

func legacyDaemonAction(cctx *cli.Context, fxOptions fx.Option) error {
	isLite := cctx.Bool("lite")
	isMirValidator := cctx.Bool("mir-validator")

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

	genesis := node.Options()
	if len(genBytes) > 0 {
		genesis = node.Override(new(modules.Genesis), modules.LoadGenesis(genBytes))
	}
	if cctx.String(makeGenFlag) != "" {
		if cctx.String(preTemplateFlag) == "" {
			return xerrors.Errorf("must also pass file with genesis template to `--%s`", preTemplateFlag)
		}
		genesis = node.Override(new(modules.Genesis), testing.MakeGenesis(cctx.String(makeGenFlag), cctx.String(preTemplateFlag)))
	}

	shutdownChan := make(chan struct{})

	// If the daemon is started in "lite mode", provide a  Gateway
	// for RPC calls
	liteModeDeps := node.Options()
	if isLite {
		gapi, closer, err := lcli.GetGatewayAPI(cctx)
		if err != nil {
			return err
		}

		defer closer()
		liteModeDeps = node.Override(new(lapi.Gateway), gapi)
	}

	// some libraries like ipfs/go-ds-measure and ipfs/go-ipfs-blockstore
	// use ipfs/go-metrics-interface. This injects a Prometheus exporter
	// for those. Metrics are exported to the default registry.
	if err := metricsprom.Inject(); err != nil {
		log.Warnf("unable to inject prometheus ipfs/go-metrics exporter; some metrics will be unavailable; err: %s", err)
	}

	var api lapi.FullNode

	//////////////////////////////////////////
	//// BEGIN REFACTORED NODE CONSTRUCTION //
	//////////////////////////////////////////

	convertedFxOptions, err := node.ConvertToFxOptions(
		node.FullAPI(&api, node.Lite(isLite), node.MirValidator(isMirValidator)),

		node.Base(),
		node.Repo(r),

		node.Override(new(dtypes.Bootstrapper), isBootstrapper),
		node.Override(new(dtypes.ShutdownChan), shutdownChan),

		genesis,
		liteModeDeps,

		node.ApplyIf(
			func(s *node.Settings) bool { return build.IsMirConsensus() },
			node.Override(new(consensus.Consensus), mir.NewConsensus),
			node.Override(new(store.WeightFunc), mir.Weight),
			node.Override(new(stmgr.Executor), consensus.NewTipSetExecutor(mir.RewardFunc)),
		),

		node.ApplyIf(func(s *node.Settings) bool { return cctx.IsSet("api") },
			node.Override(node.SetApiEndpointKey, func(lr repo.LockedRepo) error {
				apima, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/" +
					cctx.String("api"))
				if err != nil {
					return err
				}
				return lr.SetAPIEndpoint(apima)
			})),
		node.ApplyIf(func(s *node.Settings) bool { return !cctx.Bool("bootstrap") },
			node.Unset(node.RunPeerMgrKey),
			node.Unset(new(*peermgr.PeerMgr)),
		),
	)
	if err != nil {
		return xerrors.Errorf("converting options: %w", err)
	}

	stop, err := node.NewFromFxOptions(ctx, fx.Options(convertedFxOptions, fxOptions))
	if err != nil {
		return xerrors.Errorf("initializing node: %w", err)
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
		node.ShutdownHandler{Component: "node", StopFunc: stop},
	)
	<-finishCh // fires when shutdown is complete.

	// TODO: properly parse api endpoint (or make it a URL)
	return nil
}
