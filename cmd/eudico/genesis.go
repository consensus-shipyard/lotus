package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/consensus-shipyard/go-ipc-types/types"

	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/genesis"
)

var genesisCmd = &cli.Command{
	Name:        "genesis",
	Description: "manipulate lotus genesis template",
	Subcommands: []*cli.Command{
		genesisNewCmd,
		genesisCatCmd,
	},
}

var genesisNewCmd = &cli.Command{
	Name:        "new",
	Description: "create new genesis template",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet-id",
			Usage: "The ID of the subnet",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return xerrors.New("genesis new [genesis.json]")
		}
		s := cctx.String("subnet-id")
		if s == "" {
			s = types.RootStr
			fmt.Printf("Empty subnet ID is provided, %s network will be used\n", s)
		}

		id, err := types.NewSubnetIDFromString(s)
		if err != nil {
			return xerrors.Errorf("incorrect subnet ID %s: %w", err)
		}

		out := genesis.Template{
			NetworkVersion:   build.GenesisNetworkVersion,
			Accounts:         []genesis.Actor{},
			Miners:           []genesis.Miner{},
			VerifregRootKey:  gen.DefaultVerifregRootkeyActor,
			RemainderAccount: gen.DefaultRemainderAccountActor,
			NetworkName:      id.String(),
		}

		genb, err := json.MarshalIndent(&out, "", "  ")
		if err != nil {
			return err
		}

		genf, err := homedir.Expand(cctx.Args().First())
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(genf, genb, 0644); err != nil {
			return err
		}

		return nil
	},
}

var genesisCatCmd = &cli.Command{
	Name:        "cat",
	Description: "reads and prints the genesis file",
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return xerrors.New("genesis cat [genesis.json]")
		}
		fname := cctx.Args().First()
		out, err := ioutil.ReadFile(fname)
		if err != nil {
			return xerrors.Errorf("failed to read genesis file %s: %w", fname, err)
		}

		var gen genesis.Template
		if err = json.Unmarshal(out, &gen); err != nil {
			return xerrors.Errorf("failed to decode genesis file: %w", err)
		}

		fmt.Println("Version: ", gen.NetworkVersion)
		fmt.Println("Subnet ID: ", gen.NetworkName)
		fmt.Printf("Remainder account: %s:%d:%s\n", gen.RemainderAccount.Type, gen.RemainderAccount.Balance, string(gen.RemainderAccount.Meta))
		fmt.Printf("Root key: %s:%d:%s\n", gen.VerifregRootKey.Type, gen.VerifregRootKey.Balance, string(gen.VerifregRootKey.Meta))
		fmt.Println("Timestamp: ", gen.Timestamp)
		if len(gen.Miners) > 0 {
			fmt.Println("Miners:")
			for _, v := range gen.Miners {
				fmt.Printf("\t- %s\n", v.ID)
			}
		}
		if len(gen.Accounts) > 0 {
			fmt.Println("Accounts:")
			for _, v := range gen.Accounts {
				fmt.Printf("\t- %s:%d:%s\n", v.Type, v.Balance, string(v.Meta))
			}
		}

		return nil
	},
}
