package main

import (
	"fmt"

	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/subnetactor"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
)

var ipcCmds = &cli.Command{
	Name:  "ipc",
	Usage: "Commands to interact with IPC actors",
	Subcommands: []*cli.Command{
		addCmd,
	},
}

var addCmd = &cli.Command{
	Name:      "add-subnet",
	Usage:     "Spawn a new subnet in network",
	ArgsUsage: "[stake amount]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "from",
			Usage: "optionally specify the account to send funds from",
		},
		&cli.IntFlag{
			Name:  "checkpoint-period",
			Usage: "optionally specify checkpointing period for subnet",
			Value: 100, // 10 blocks for default checkpoint period, for now
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "specify name for the subnet",
		},
		&cli.Uint64Flag{
			Name:  "min-validators",
			Usage: "optionally specify minimum number of validators in subnet",
			Value: 0,
		},
		&cli.IntFlag{
			Name:  "finality-threshold",
			Usage: "the number of epochs to wait before considering a change final (default = 5 epochs)",
			Value: 1, // 1 by default, we are considering a BFT consensus.
		},
	},
	Action: func(cctx *cli.Context) error {

		api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()

		if cctx.Args().Len() != 0 {
			return lcli.ShowHelp(cctx, fmt.Errorf("'add' expects no arguments, just a set of flags"))
		}

		ctx := lcli.ReqContext(cctx)

		// Try to get default address first
		addr, _ := api.WalletDefaultAddress(ctx)
		if from := cctx.String("from"); from != "" {
			addr, err = address.NewFromString(from)
			if err != nil {
				return err
			}
		}

		if !cctx.IsSet("name") {
			return lcli.ShowHelp(cctx, fmt.Errorf("no name for subnet specified"))
		}
		subnetName := cctx.String("name")

		// the parent is the current subnet.
		networkName, err := api.StateNetworkName(ctx)
		if err != nil {
			return err
		}
		parent, err := sdk.NewSubnetIDFromString(string(networkName))
		if err != nil {
			return err
		}

		// subnet-specific configurations from cli flags
		minVals := cctx.Uint64("min-validators")
		chp := abi.ChainEpoch(cctx.Int("checkpoint-period"))
		finalityThreshold := abi.ChainEpoch(cctx.Int("finality-threshold"))

		// get the default gateway address
		gwAddr, err := address.IDFromAddress(genesis.DefaultIPCGatewayAddr)
		if err != nil {
			return err
		}

		params := subnetactor.ConstructParams{
			Parent:            parent,
			Name:              subnetName,
			IPCGatewayAddr:    gwAddr,
			CheckPeriod:       chp,
			FinalityThreshold: finalityThreshold,
			MinValidators:     minVals,
			MinValidatorStake: abi.TokenAmount(types.MustParseFIL("1FIL")),
			Consensus:         subnetactor.Mir,
		}
		actorAddr, err := api.IPCAddSubnetActor(ctx, addr, params)
		if err != nil {
			return err
		}

		fmt.Printf("[*] subnet actor deployed as %v and new subnet available with ID=%v\n\n",
			actorAddr, sdk.NewSubnetID(parent, actorAddr))
		fmt.Printf("remember to join and register your subnet for it to be discoverable\n")
		return nil
	},
}
