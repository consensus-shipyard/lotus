package tspow

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/types"
)

var _ consensus.Consensus = (*TSPoW)(nil)

type TSPoW struct {
}

func NewTSPoWConsensus() *TSPoW {
	return &TSPoW{}
}

func (tsp *TSPoW) ValidateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error) {
	panic("ValidateBlockHeader not implemented")
}

func (tsp *TSPoW) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	panic("ValidateBlock not implemented")
}

func (tsp *TSPoW) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	panic("IsEpochBeyondCurrMax not implemented")
}

func (tsp *TSPoW) CreateBlock(ctx context.Context, w api.Wallet, bt *api.BlockTemplate) (*types.FullBlock, error) {
	panic("CreateBlock not implemented")
}
