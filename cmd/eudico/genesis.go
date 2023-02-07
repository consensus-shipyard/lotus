package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

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
	genesisTemplateFileName = "genesis/genesis.json"
)

var genesisCmd = &cli.Command{
	Name:        "genesis",
	Description: "manipulate lotus genesis template",
	Subcommands: []*cli.Command{
		genesisNewCmd,
	},
}

var genesisNewCmd = &cli.Command{
	Name:        "new",
	Description: "create new genesis from the template and store it in a car file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "subnet-id",
			Value: types.RootStr,
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
		subnetID, err := types.NewSubnetIDFromString(sid)
		if err != nil {
			return xerrors.Errorf("incorrect subnet ID %s: %w", sid, err)
		}

		e, err := os.Executable()
		if err != nil {
			return err
		}

		tmplFilePath := filepath.Join(filepath.Dir(e), genesisTemplateFileName)
		tmplBytes, err := ioutil.ReadFile(tmplFilePath)
		if err != nil {
			return xerrors.Errorf("failed to read template %s: %w", tmplFilePath, err)
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

		genFilePath, err := homedir.Expand(cctx.Args().First() + ".tmp")
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(genFilePath, tmplBytes, 0644); err != nil {
			return xerrors.Errorf("failed to create genesis file %s: %w", genFilePath, err)
		}

		jrnl := journal.NilJournal()
		bstor := blockstore.WrapIDStore(blockstore.NewMemorySync())
		sbldr := vm.Syscalls(ffiwrapper.ProofVerifier)

		_, err = testing.MakeGenesis(cctx.String("out"), genFilePath)(bstor, sbldr, jrnl)()
		if err != nil {
			return err
		}

		return os.Remove(cctx.Args().First() + ".tmp")
	},
}
