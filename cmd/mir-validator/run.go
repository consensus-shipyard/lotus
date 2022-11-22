package main

import (
	"context"
	_ "net/http/pprof"
	"path/filepath"

	"github.com/urfave/cli/v2"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	mirkv "github.com/filecoin-project/lotus/chain/consensus/mir/db/kv"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/ulimit"
	"github.com/filecoin-project/lotus/metrics"
)

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start a mir validator process",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "default-key",
			Value: true,
			Usage: "use default wallet's key",
		},
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account used for the validator",
		},
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.BoolFlag{
			Name:  "manage-fdlimit",
			Usage: "manage open file limit",
			Value: true,
		},
		&cli.IntFlag{
			Name:  "checkpoint-period",
			Usage: "Checkpoint period",
			Value: 8,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx, _ := tag.New(lcli.DaemonContext(cctx),
			tag.Insert(metrics.Version, build.BuildVersion),
			tag.Insert(metrics.Commit, build.CurrentCommit),
			tag.Insert(metrics.NodeType, "miner"),
		)
		// Register all metric views
		if err := view.Register(
			metrics.MinerNodeViews...,
		); err != nil {
			log.Fatalf("Cannot register the view: %v", err)
		}
		// Set the metric to one so it is published to the exporter
		stats.Record(ctx, metrics.LotusInfo.M(1))

		nodeApi, ncloser, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return xerrors.Errorf("getting full node api: %w", err)
		}
		defer ncloser()

		v, err := nodeApi.Version(ctx)
		if err != nil {
			return err
		}

		// check if validator has been initialized.
		if err := initCheck(cctx.String("repo")); err != nil {
			return err
		}

		if cctx.Bool("manage-fdlimit") {
			if _, _, err := ulimit.ManageFdLimit(); err != nil {
				log.Errorf("setting file descriptor limit: %s", err)
			}
		}

		if v.APIVersion != api.FullAPIVersion1 {
			return xerrors.Errorf("lotus-daemon API version doesn't match: expected: %s", api.APIVersion{APIVersion: api.FullAPIVersion1})
		}

		log.Info("Checking full node sync status")

		if !cctx.Bool("nosync") {
			if err := lcli.SyncWait(ctx, &v0api.WrapperV1Full{FullNode: nodeApi}, false); err != nil {
				return xerrors.Errorf("sync wait: %w", err)
			}
		}

		// Validator identity.
		validator, err := validatorIDFromFlag(ctx, cctx, nodeApi)
		if err != nil {
			return err
		}

		// Membership config.
		// TODO: Make this configurable.
		membershipCfg := filepath.Join(cctx.String("repo"), MembershipCfgPath)

		// Checkpoint period.
		checkpointPeriod := cctx.Int("checkpoint-period")

		h, err := newLp2pHost(cctx.String("repo"))
		if err != nil {
			return err
		}

		log.Info("Mir libp2p host listening in the following addresses:")
		for _, a := range h.Addrs() {
			log.Info(a)
		}

		// Initialize Mir's DB.
		dbPath := filepath.Join(cctx.String("repo"), LevelDSPath)
		ds, err := mirkv.NewLevelDB(dbPath, false)
		if err != nil {
			log.Fatalf("error initializing mir datastore: %w", err)
		}

		log.Infow("Starting mining with validator", "validator", validator)
		cfg := mir.NewConfig(mir.MembershipFromFile(membershipCfg), dbPath, checkpointPeriod)
		return mir.Mine(ctx, validator, h, nodeApi, ds, cfg)
	},
}

func validatorIDFromFlag(ctx context.Context, cctx *cli.Context, nodeApi api.FullNode) (address.Address, error) {
	var (
		validator address.Address
		err       error
	)

	if cctx.Bool("default-key") {
		validator, err = nodeApi.WalletDefaultAddress(ctx)
		if err != nil {
			return address.Undef, err
		}
	}
	if cctx.String("from") != "" {
		validator, err = address.NewFromString(cctx.String("from"))
		if err != nil {
			return address.Undef, err
		}
	}
	if validator == address.Undef {
		return address.Undef, xerrors.Errorf("no validator address specified as first argument for validator")
	}

	return validator, nil
}
