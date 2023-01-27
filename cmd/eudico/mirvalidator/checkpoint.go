package mirvalidator

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"
	"path/filepath"

	"github.com/ipfs/go-datastore"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir/pkg/checkpoint"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	mirkv "github.com/filecoin-project/lotus/chain/consensus/mir/db/kv"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/metrics"
)

var checkCmd = &cli.Command{
	Name:  "checkpoint",
	Usage: "Manage Mir checkpoints",
	Subcommands: []*cli.Command{
		importCheckCmd,
		exportCheckCmd,
	},
}

var importCheckCmd = &cli.Command{
	Name:  "import",
	Usage: "Imports checkpoint from file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "file",
			Aliases: []string{"f"},
			Usage:   "optionally specify the account used for the validator",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx, _ := tag.New(lcli.DaemonContext(cctx),
			tag.Insert(metrics.Version, build.BuildVersion),
			tag.Insert(metrics.Commit, build.CurrentCommit),
			tag.Insert(metrics.NodeType, "miner"),
		)

		repoFlag := cctx.String("repo")
		fileFlag := cctx.String("file")

		// check if validator has been initialized.
		if err := initCheck(repoFlag); err != nil {
			return err
		}

		// Initialize Mir's DB.
		dbPath := filepath.Join(repoFlag, LevelDSPath)
		ds, err := mirkv.NewLevelDB(dbPath, false)
		if err != nil {
			return fmt.Errorf("error initializing mir datastore: %s", err)
		}

		_, err = checkpointFromFile(ctx, ds, fileFlag)
		log.Infof("Import checkpoint from file %s", fileFlag)
		return err
	},
}

var exportCheckCmd = &cli.Command{
	Name:  "export",
	Usage: "Exports checkpoint to file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "output",
			Usage: "optionally specify the output for the checkpoint",
		},
		&cli.IntFlag{
			Name:  "height",
			Usage: "optionally specify the height for the checkpoint to export. If not specified the latest one will be exported",
			Value: 0,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx, _ := tag.New(lcli.DaemonContext(cctx),
			tag.Insert(metrics.Version, build.BuildVersion),
			tag.Insert(metrics.Commit, build.CurrentCommit),
			tag.Insert(metrics.NodeType, "miner"),
		)

		repoFlag := cctx.String("repo")

		// check if validator has been initialized.
		if err := initCheck(repoFlag); err != nil {
			return err
		}

		// Initialize Mir's DB.
		dbPath := filepath.Join(repoFlag, LevelDSPath)
		ds, err := mirkv.NewLevelDB(dbPath, true)
		if err != nil {
			return fmt.Errorf("error initializing mir datastore: %s", err)
		}

		height := abi.ChainEpoch(cctx.Int("height"))
		ch, err := mir.GetCheckpointByHeight(ctx, ds, height, nil)
		if err != nil {
			return fmt.Errorf("error getting checkpoint by height: %s", err)
		}
		path := cctx.String("file")
		heightStr := height.String()
		if path == "" {
			if height == 0 {
				heightStr = "latest"
			}
			path = "./checkpoint-height-" + heightStr + ".chkp"
		}
		log.Infof("Exporting checkpoint for height %s to file %s", heightStr, path)
		return mir.CheckpointToFile(ch, path)
	},
}

func checkpointFromFile(ctx context.Context, ds datastore.Datastore, path string) (*checkpoint.StableCheckpoint, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error checkpoint from file: %s", err)
	}
	ch := &checkpoint.StableCheckpoint{}
	err = ch.Deserialize(b)
	if err != nil {
		return nil, fmt.Errorf("error deserializing checkpoint from file: %s", err)
	}
	snapshot := &mir.Checkpoint{}
	if err := snapshot.FromBytes(ch.Snapshot.AppData); err != nil {
		return nil, xerrors.Errorf("error getting checkpoint snapshot from mir checkpoint: %s", err)
	}
	// always flush the checkpoint in database when importing so we have posterior knowledge of it.
	if err := ds.Put(ctx, mir.HeightCheckIndexKey(snapshot.Height), b); err != nil {
		return nil, xerrors.Errorf("error flushing checkpoint for height %d in datastore: %w", snapshot.Height, err)
	}
	return ch, nil
}
