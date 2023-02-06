package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/consensus-shipyard/go-ipc-types/types"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/node/modules/testing"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
)

const (
	defaultTemplateFile = "./eudico/template.json"
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
	Description: "create new genesis from the template and store it in a car file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet-id",
			Usage: "The ID of the subnet",
		},
		&cli.StringFlag{
			Name:  "template-file",
			Usage: "genesis template file [template.json]",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Args().Present() {
			return xerrors.New("genesis new [genesis]")
		}
		sid := cctx.String("subnet-id")
		if sid == "" {
			sid = types.RootStr
			fmt.Printf("Empty subnet ID is provided, %s network will be used\n", sid)
		}

		subnetID, err := types.NewSubnetIDFromString(sid)
		if err != nil {
			return xerrors.Errorf("incorrect subnet ID %s: %w", err)
		}

		tmplFilePath := cctx.String("template-file")
		if tmplFilePath == "" {
			tmplFilePath = defaultTemplateFile
		}

		tmplBytes, err := ioutil.ReadFile(tmplFilePath)
		if err != nil {
			return xerrors.Errorf("failed to read template file %s: %w", tmplFilePath, err)
		}

		var tmpl genesis.Template
		if err := json.Unmarshal(tmplBytes, &tmpl); err != nil {
			return err
		}

		tmpl.NetworkName = subnetID.String()

		tmplBytes, err = json.MarshalIndent(&tmpl, "", "  ")
		if err != nil {
			return err
		}

		genFilePath, err := homedir.Expand(cctx.Args().First() + ".car")
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(genFilePath, tmplBytes, 0644); err != nil {
			return xerrors.Errorf("failed to create genesis file %s: %w", genFilePath, err)
		}

		jrnl := journal.NilJournal()
		bstor := blockstore.WrapIDStore(blockstore.NewMemorySync())
		sbldr := vm.Syscalls(ffiwrapper.ProofVerifier)

		_, err = testing.MakeGenesis(cctx.Args().First()+".car", genFilePath)(bstor, sbldr, jrnl)()
		return err
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
