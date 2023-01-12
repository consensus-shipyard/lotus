package mirvalidator

import (
	"github.com/urfave/cli/v2"

	cliutil "github.com/filecoin-project/lotus/cli/util"
)

var ValidatorCmd = &cli.Command{
	Name:  "validator",
	Usage: "Run and manage a mir validator process",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "checkpoints-repo",
			EnvVars: []string{"CHECKPOINTS_REPO"},
			Hidden:  true,
		},
		cliutil.FlagVeryVerbose,
	},
	Subcommands: []*cli.Command{
		runCmd,
		cfgCmd,
		checkCmd,
	},
}
