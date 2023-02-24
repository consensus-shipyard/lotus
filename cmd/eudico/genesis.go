package main

import (
	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/eudico-core/genesis"
)

var genesisCmd = &cli.Command{
	Name:        "genesis",
	Description: "manipulate eudico genesis template",
	Subcommands: []*cli.Command{
		genesisNewCmd,
	},
}

var genesisNewCmd = &cli.Command{
	Name:        "new",
	Description: "create new genesis from the Spacenet template and store it in a car file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet-id",
			Value: sdk.RootStr,
			Usage: "The ID of the subnet",
		},
		&cli.StringFlag{
			Name:    "out",
			Aliases: []string{"o"},
			Value:   "genesis.car",
			Usage:   "write output to `FILE`",
		},
	},
	Action: func(cctx *cli.Context) error {
		sid := cctx.String("subnet-id")
		subnetID, err := sdk.NewSubnetIDFromString(sid)
		if err != nil {
			return xerrors.Errorf("incorrect subnet ID %s: %w", sid, err)
		}

		err = genesis.MakeGenesisCar(cctx.Context, cctx.String("out"), subnetID.String())
		if err != nil {
			return xerrors.Errorf("failed to make genesis: %w", err)
		}

		return nil
	},
}
