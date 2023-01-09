package main

import (
	"github.com/filecoin-project/lotus/eudico/global"
	"github.com/urfave/cli/v2"
)

var mirCmd = &cli.Command{
	Name:  "mir",
	Usage: "Mir consensus",
	Subcommands: []*cli.Command{
		daemonCmd(global.MirConsensus),
	},
}
