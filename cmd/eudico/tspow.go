package main

import (
	"github.com/filecoin-project/lotus/eudico-core/global"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

var tpowCmd = &cli.Command{
	Name:  "tspow",
	Usage: "TipSet PoW consensus testbed",
	Subcommands: []*cli.Command{
		tpowMinerCmd,
		daemonCmd(global.TSPoWConsensus),
	},
}

var tpowMinerCmd = &cli.Command{
	Name:  "miner",
	Usage: "run tspow consensus miner",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "default-key",
			Value: true,
			Usage: "use default wallet's key",
		},
	},
	Action: func(cctx *cli.Context) error {
		api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := cliutil.ReqContext(cctx)

		var miner address.Address

		if cctx.Bool("default-key") {
			miner, err = api.WalletDefaultAddress(ctx)
			if err != nil {
				return err
			}
		} else {
			miner, err := address.NewFromString(cctx.Args().First())
			if err != nil {
				return err
			}
			if miner == address.Undef {
				return xerrors.Errorf("no miner address specified to start mining")
			}
		}

		log.Infow("Starting mining with miner", "miner", miner)
		return tspow.Mine(ctx, miner, api)
	},
}
