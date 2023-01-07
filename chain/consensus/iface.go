package consensus

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

type Consensus interface {
	// ValidateBlockHeader is called by peers when they receive a new block through the network.
	//
	// This is a fast sanity-check validation performed by the PubSub protocol before delivering
	// it to the syncer. It checks that the block has the right format and it performs
	// other consensus-specific light verifications like ensuring that the block is signed by
	// a valid miner, or that it includes all the data required for a full verification.
	ValidateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error)

	// ValidateBlock is called by the syncer to determine if to accept a block or not.
	//
	// It performs all the checks needed by the syncer to accept
	// the block (signature verifications, VRF checks, message validity, etc.)
	ValidateBlock(ctx context.Context, b *types.FullBlock) (err error)

	// IsEpochBeyondCurrMax is used to configure the fork rules for longest-chain
	// consensus protocols.
	IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool

	// CreateBlock implements all the logic required to propose and assemble a new Filecoin block.
	//
	// This function encapsulate all the consensus-specific actions to propose a new block
	// such as the ordering of transactions, the inclusion of consensus proofs, the signature
	// of the block, etc.
	CreateBlock(ctx context.Context, w api.Wallet, bt *api.BlockTemplate) (*types.FullBlock, error)
}
