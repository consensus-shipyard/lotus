package main

import (
	"github.com/filecoin-project/lotus/eudico/fxmodules"
	"github.com/urfave/cli/v2"
)

var filcnsCmd = &cli.Command{
	Name:  "filcns",
	Usage: "Filecoin Consensus consensus testbed",
	Subcommands: []*cli.Command{
		daemonCmd(fxmodules.ExpectedConsensus),
	},
}
