package genesis

import (
	"context"
	"encoding/json"
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

func MakeGenesis(ctx context.Context, outFilePath string, subnetID string) error {
	e, err := os.Executable()
	if err != nil {
		return err
	}

	tmplFilePath := filepath.Join(filepath.Dir(e), genesisTemplateFilePath)
	tmplBytes, err := ioutil.ReadFile(tmplFilePath)
	if err != nil {
		return xerrors.Errorf("failed to read template %s: %w", tmplFilePath, err)
	}

	var tmpl genesis.Template
	if err := json.Unmarshal(tmplBytes, &tmpl); err != nil {
		return err
	}
	tmpl.NetworkName = subnetID

	jrnl := journal.NilJournal()
	bs := blockstore.WrapIDStore(blockstore.NewMemorySync())
	sbldr := vm.Syscalls(ffiwrapper.ProofVerifier)

	b, err := lotusGenesis.MakeGenesisBlock(ctx, jrnl, bs, sbldr, tmpl)
	if err != nil {
		return xerrors.Errorf("failed to make genesis block: %w", err)
	}

	f, err := os.OpenFile(outFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	offl := offline.Exchange(bs)
	blkserv := blockservice.New(bs, offl)
	dserv := merkledag.NewDAGService(blkserv)

	if err := car.WriteCarWithWalker(ctx, dserv, []cid.Cid{b.Genesis.Cid()}, f, gen.CarWalkFunc); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}
	return nil
}
