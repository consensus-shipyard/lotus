package genesis

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
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
	genesisTemplateFilePath = "eudico-core/genesis/genesis.json"
)

func MakeGenesisCar(ctx context.Context, outFilePath string, subnetID string) error {
	f, err := os.OpenFile(outFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if err := makeGenesis(ctx, f, subnetID); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return nil
}

func MakeGenesisBytes(ctx context.Context, subnetID string) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := makeGenesis(ctx, buf, subnetID); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil

}

func MakeGenesisTemplate(subnetID string) (genesis.Template, error) {
	e, err := os.Executable()
	if err != nil {
		return genesis.Template{}, err
	}

	tmplFilePath := filepath.Join(filepath.Dir(e), genesisTemplateFilePath)
	tmplBytes, err := ioutil.ReadFile(tmplFilePath)
	if err != nil {
		return genesis.Template{}, xerrors.Errorf("failed to read template %s: %w", tmplFilePath, err)
	}

	var tmpl genesis.Template
	if err := json.Unmarshal(tmplBytes, &tmpl); err != nil {
		return genesis.Template{}, err
	}
	tmpl.NetworkName = subnetID
	return tmpl, nil
}

func makeGenesis(ctx context.Context, w io.Writer, subnetID string) error {
	tmpl, err := MakeGenesisTemplate(subnetID)
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
