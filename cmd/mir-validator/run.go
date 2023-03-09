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
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	mirkv "github.com/filecoin-project/lotus/chain/consensus/mir/db/kv"
	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/eudico-core/global"
	"github.com/filecoin-project/lotus/lib/ulimit"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	mirlibp2p "github.com/filecoin-project/mir/pkg/net/libp2p"
	t "github.com/filecoin-project/mir/pkg/types"
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
			Name:  "init-height",
			Usage: "checkpoint height from which to start the validator",
			Value: 0,
		},
		&cli.StringFlag{
			Name:  "init-checkpoint",
			Usage: "pass initial checkpoint as a file (it overwrites 'init-height' flag)",
		},
		&cli.IntFlag{
			Name:  "segment-length",
			Usage: "The length of an ISS segment. Must not be negative",
			Value: 1,
		},
	},
	Action: func(cctx *cli.Context) error {
		global.SetConsensusAlgorithm(global.MirConsensus)

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
		validatorID, err := validatorIDFromFlag(ctx, cctx, nodeApi)
		if err != nil {
			return err
		}

		// Membership config.
		// TODO: Make this configurable.
		membershipFile := filepath.Join(cctx.String("repo"), MembershipPath)

		// Segment length period.
		segmentLength := cctx.Int("segment-length")

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
			return xerrors.Errorf("error initializing mir datastore: %w", err)
		}

		// get initial checkpoint
		var initCh *checkpoint.StableCheckpoint
		if cctx.String("init-checkpoint") != "" {
			initCh, err = checkpointFromFile(ctx, ds, cctx.String("init-checkpoint"))
			if err != nil {
				return xerrors.Errorf("failed to get initial checkpoint from file: %s", err)
			}
			log.Info("Initializing mir validator from checkpoint provided in file: %s", cctx.String("init-checkpoint"))
		} else if cctx.Int("init-height") != 0 {
			initCh, err = mir.GetCheckpointByHeight(ctx, ds, abi.ChainEpoch(cctx.Int("init-height")), nil)
			if err != nil {
				return xerrors.Errorf("failed to get initial checkpoint from file: %s", err)
			}
			log.Info("Initializing mir validator from checkpoint in height: %d", cctx.Int("init-height"))
		}

		log.Infow("Starting mining with validator", "validator", validatorID)

		membership := membership.NewFileMembership(membershipFile)

		var netLogger = mir.NewLogger(validatorID.String())
		netTransport := mirlibp2p.NewTransport(mirlibp2p.DefaultParams(), t.NodeID(validatorID.String()), h, netLogger)

		cfg := mir.NewConfig(
			dbPath,
			initCh,
			cctx.String("checkpoints-repo"),
			segmentLength,
		)

		return mir.Mine(ctx, validatorID, netTransport, nodeApi, ds, membership, cfg)
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
