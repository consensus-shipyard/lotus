package genesis

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	offline "github.com/ipfs/go-ipfs-exchange-offline"
	"github.com/ipfs/go-merkledag"
	"github.com/ipld/go-car"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/gen"
	lotusGenesis "github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
)

const (
	defaultTemplateFilePath = "eudico-core/genesis/genesis.json"
)

type SubnetParams struct {
	TemplatePath   string
	OutputFilePath string
	SubnetID       string
	IPCAgentURL    string
}

func MakeGenesisCar(ctx context.Context, params *SubnetParams) error {
	f, err := os.OpenFile(params.OutputFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if err := makeGenesis(ctx, f, params); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

func MakeGenesisTemplate(params *SubnetParams) (genesis.Template, error) {
	var tmplPath string
	if params.TemplatePath == "" {
		e, err := os.Executable()
		if err != nil {
			return genesis.Template{}, err
		}
		tmplPath = filepath.Join(filepath.Dir(e), defaultTemplateFilePath)
	}

	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		return genesis.Template{}, xerrors.Errorf("failed to read template %s: %w", tmplPath, err)
	}

	var tmpl genesis.Template
	if err := json.Unmarshal(tmplBytes, &tmpl); err != nil {
		return genesis.Template{}, err
	}
	tmpl.NetworkName = params.SubnetID
	return tmpl, nil
}

func makeGenesis(ctx context.Context, w io.Writer, params *SubnetParams) error {
	tmpl, err := MakeGenesisTemplate(params)
	if err != nil {
		return err
	}
	jrnl := journal.NilJournal()
	bs := blockstore.WrapIDStore(blockstore.NewMemorySync())
	sbldr := vm.Syscalls(ffiwrapper.ProofVerifier)

	b, err := lotusGenesis.MakeGenesisBlock(ctx, jrnl, bs, sbldr, tmpl, true)
	if err != nil {
		return xerrors.Errorf("failed to make genesis block: %w", err)
	}

	offl := offline.Exchange(bs)
	blkserv := blockservice.New(bs, offl)
	dserv := merkledag.NewDAGService(blkserv)

	if err := car.WriteCarWithWalker(ctx, dserv, []cid.Cid{b.Genesis.Cid()}, w, gen.CarWalkFunc); err != nil {
		return err
	}

	return nil
}
