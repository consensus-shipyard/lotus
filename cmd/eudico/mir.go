package main

import (
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/lotus/cmd/eudico/mirvalidator"
	"github.com/filecoin-project/lotus/eudico-core/global"
)

var mirCmd = &cli.Command{
	Name:  "mir",
	Usage: "Mir consensus",
	Subcommands: []*cli.Command{
		daemonCmd(global.MirConsensus),
		mirvalidator.ValidatorCmd,
	},
}
