package main

import (
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/consensus-shipyard/go-ipc-types/sdk"

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
		&cli.StringFlag{
			Name:    "template",
			Aliases: []string{"tmp"},
			Usage:   "genesis template for the subnet",
		},
		&cli.StringFlag{
			Name:  "ipcagent-url",
			Usage: "The URL of IPC Agent interface",
		},
	},
	Action: func(cctx *cli.Context) error {
		sid := cctx.String("subnet-id")
		subnetID, err := sdk.NewSubnetIDFromString(sid)
		if err != nil {
			return xerrors.Errorf("incorrect subnet ID %s: %w", sid, err)
		}

		params := &genesis.SubnetParams{
			TemplatePath:   cctx.String("template"),
			OutputFilePath: cctx.String("out"),
			IPCAgentURL:    cctx.String("ipcagent-url"),
			SubnetID:       subnetID.String(),
		}
		if err = genesis.MakeGenesisCar(cctx.Context, params); err != nil {
			return xerrors.Errorf("failed to make genesis: %w", err)
		}

		return nil
	},
}
