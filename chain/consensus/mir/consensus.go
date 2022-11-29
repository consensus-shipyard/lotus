//go:generate go run ./gen/gen.go

// Package mir implements Mir integration in Lotus as an alternative consensus.
package mir

import (
	"context"
	"crypto"
	"fmt"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	lapi "github.com/filecoin-project/lotus/api"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/lib/async"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

var _ consensus.Consensus = &Mir{}

var RewardFunc = func(ctx context.Context, vmi vm.Interface, em stmgr.ExecMonitor,
	epoch abi.ChainEpoch, ts *types.TipSet, params *reward.AwardBlockRewardParams) error {
	// TODO: No RewardFunc implemented for mir yet
	return nil
}

type Mir struct {
	beacon  beacon.Schedule
	sm      *stmgr.StateManager
	genesis *types.TipSet
	cache   *mirCache
}

func NewConsensus(
	ctx context.Context,
	sm *stmgr.StateManager,
	ds dtypes.MetadataDS,
	b beacon.Schedule,
	g chain.Genesis,
	badBlock *chain.BadBlockCache,
	netName dtypes.NetworkName,
) (consensus.Consensus, error) {
	return &Mir{
		beacon:  b,
		sm:      sm,
		genesis: g,
		cache:   newDsBlkCache(ds, badBlock),
	}, nil
}

// CreateBlock creates a Filecoin block from the block template provided by Mir.
func (bft *Mir) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	pts, err := bft.sm.ChainStore().LoadTipSet(ctx, bt.Parents)
	if err != nil {
		return nil, fmt.Errorf("failed to load parent tipset: %w", err)
	}

	st, recpts, err := bft.sm.TipSetState(ctx, pts)
	if err != nil {
		return nil, fmt.Errorf("failed to load tipset state: %w", err)
	}

	next := &types.BlockHeader{
		Miner:         builtin.SystemActorAddr, // Mir's blocks are not signed, we use system addr as miner.
		Parents:       bt.Parents.Cids(),
		Ticket:        bt.Ticket,
		ElectionProof: bt.Eproof,

		BeaconEntries: bt.BeaconValues,
		Height:        bt.Epoch,
		// Each validator in Mir be assembling the block with a different
		// timestamp. To avoid validators from pushing blocks with different
		// timestamps that lead to different CIDs, we use the epoch as
		// a timestamp for now.
		// TODO: Consider exporting a batch timestamp from Mir and use it
		// for the block timestamp.
		Timestamp:             uint64(bt.Epoch),
		WinPoStProof:          bt.WinningPoStProof,
		ParentStateRoot:       st,
		ParentMessageReceipts: recpts,
	}
	blsMessages, secpkMessages, err := consensus.MsgsFromBlockTemplate(ctx, bft.sm, next, pts, bt)
	if err != nil {
		return nil, xerrors.Errorf("failed to process messages from block template: %w", err)
	}

	return &types.FullBlock{
		Header:        next,
		BlsMessages:   blsMessages,
		SecpkMessages: secpkMessages,
	}, nil
}

func (bft *Mir) ValidateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error) {
	if b.IsValidated() {
		return "", nil
	}

	// get the latest checkpoint in cache
	prev, err := bft.cache.getLatestCheckpoint()
	if err != nil {
		return "err_latest_checkpoint", xerrors.Errorf("couldn't get latests checkpoint: %w", err)
	}

	// if there is a checkpoint, verify it before accepting the block.
	if hasCheckpoint(b) {
		if _, err := bft.verifyCheckpointInHeader(b); err != nil {
			log.Warnf("checkpoint validation failed in block: %s", err)
			return "checkpoint_verification_failed", err
		}
	}
	// check that the block is in the right range.
	if b.Height < prev.Height {
		return "block_out_of_range", xerrors.Errorf("the height of the received block is over the latest checkpoint received")
	}
	b.SetValidated()

	return "", nil
}

func (bft *Mir) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	log.Infof("starting block validation process at @%d", b.Header.Height)

	if err := blockSanityChecks(b.Header); err != nil {
		return xerrors.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := bft.sm.ChainStore().LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
	if err != nil {
		return xerrors.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}
	if h.Height <= baseTs.Height() {
		return xerrors.Errorf("block height not greater than parent height: %d != %d", h.Height, baseTs.Height())
	}

	// TODO: Include a block drift check when the batch timestamp is included in the block.
	// Allow a small block drift
	// now := uint64(build.Clock.Now().Unix())
	// if h.Timestamp > now+build.AllowableClockDriftSecs {
	// 	return xerrors.Errorf("block was from the future (now=%d, blk=%d): %w", now, h.Timestamp, consensus.ErrTemporal)
	// }
	// if h.Timestamp > now {
	// 	log.Warn("got block from the future, but within threshold", h.Timestamp, build.Clock.Now().Unix())
	// }

	if h.Timestamp != uint64(h.Height) {
		return xerrors.Errorf("Mir blocks should include the block height as timestamp (ts=%d, height=%d)", h.Timestamp, h.Height)
	}

	pweight, err := bft.sm.ChainStore().Weight(ctx, baseTs)
	if err != nil {
		return xerrors.Errorf("getting parent weight: %w", err)
	}

	if types.BigCmp(pweight, b.Header.ParentWeight) != 0 {
		return xerrors.Errorf("parrent weight different: %s (header) != %s (computed)",
			b.Header.ParentWeight, pweight)
	}

	checkpointChk := async.Err(func() error {
		if hasCheckpoint(h) {
			ch, err := bft.verifyCheckpointInHeader(h)
			if err != nil {
				return xerrors.Errorf("error verifying checkpoint: %w", err)
			}
			if err := bft.cache.rcvCheckpoint(ch); err != nil {
				return xerrors.Errorf("error verifying unverified blocks from checkpoint: %w", err)
			}
		}

		// the genesis block can be considered as verified already.
		if h.Height != 0 {
			// we should receive all blocks, including the ones that don't include checkpoints
			// so they are conveniently verified
			// TODO: There is an attack surface here, what if a malicious peer sends two
			// blocks for the same epoch? This is handled in the cache by just accepting
			// the first one and rejecting any subsequent ones. A malicious node could
			// force a forged block to us to get us out-of-sync. While this is a hustle,
			// the worst case here is that we would have to keep restoring sync from a checkpoint
			if err := bft.cache.rcvBlock(h); err != nil {
				return xerrors.Errorf("error receiving block in cache: %w", err)
			}
		}

		return nil
	})

	asyncChecks := append(
		consensus.CommonBlkChecks(ctx, bft.sm, bft.sm.ChainStore(), b, baseTs),
		checkpointChk,
	)

	return consensus.RunAsyncChecks(ctx, asyncChecks)
}

func blockSanityChecks(h *types.BlockHeader) error {
	if h.ElectionProof.WinCount != 0 {
		return xerrors.Errorf("mir expects a zero wincount")
	}

	if h.Ticket.VRFProof != nil {
		if h.ElectionProof.VRFProof == nil {
			return xerrors.Errorf("both VRFProofs should be nil, the block includes a checkpoint")
		}
	}

	if h.Ticket.VRFProof == nil {
		if h.ElectionProof.VRFProof != nil {
			return xerrors.Errorf("if there is no ticket, then the block doesn't include a checkpoint")
		}
	}

	if h.BlockSig != nil {
		return xerrors.Errorf("mir blocks have no signature")
	}

	if h.BLSAggregate == nil {
		return xerrors.Errorf("block had nil bls aggregate signature")
	}

	if len(h.Parents) != 1 {
		return xerrors.Errorf("must have 1 parent")
	}

	if h.Miner.Protocol() != address.ID {
		return xerrors.Errorf("block had non-ID miner address")
	}

	if h.Miner != builtin.SystemActorAddr {
		return xerrors.Errorf("mir blocks include the systemActor addr as miner")
	}

	return nil
}

func (bft *Mir) verifyCheckpointInHeader(h *types.BlockHeader) (*Checkpoint, error) {
	ch, err := CheckpointFromVRFProof(h.Ticket)
	if err != nil {
		return nil, xerrors.Errorf("error getting checkpoint from ticket: %w", err)
	}
	cert, err := CertFromElectionProof(h.ElectionProof)
	if err != nil {
		return nil, xerrors.Errorf("error getting checkpoint config from election proof: %w", err)
	}
	ch = ch.AttachCert(cert)

	snap, err := UnwrapCheckpointSnapshot(ch)
	if err != nil {
		return nil, xerrors.Errorf("error unwrapping checkpoint snapshot: %w", err)
	}

	// get the latest checkpoint in cache
	prev, err := bft.cache.prevCheckpoint(snap)
	if err != nil {
		return nil, xerrors.Errorf("couldn't get previous checkpoint: %w", err)
	}
	// check that the block is in the right range.
	if h.Height < prev.Height {
		return nil, xerrors.Errorf("the height of the received block is over the latest checkpoint received")
	}

	// verify checkpoint signature
	// TODO: Right now we assume static membership and the membership is received off-chain.
	// Once we support reconfiguration (and especially if we track membership online), we should
	// check that the membership expected for the checkpoint is correct as part of the verification.
	// Here we are just getting the most recent membership according to the cert without additional
	// checks. We should probably check if the membership included in the cert is the correct one.
	if err := ch.VerifyCert(crypto.SHA256, CheckpointVerifier{}, ch.Memberships()[0]); err != nil {
		return nil, xerrors.Errorf("error verifying checkpoint signature: %w", err)
	}
	c, err := prev.Cid()
	if err != nil {
		return nil, xerrors.Errorf("error computing cid for latest checkpoint: %w", err)
	}
	// if cid.Undef this is the first checkpoint, nothing to do here.
	if c != cid.Undef {
		if snap.Parent.Cid != c || snap.Parent.Height != prev.Height {
			return nil, xerrors.Errorf("new checkpoint not pointing to the previous one: %s, %s", c, snap.Parent.Cid)
		}
	}

	return snap, nil
}

func hasCheckpoint(h *types.BlockHeader) bool {
	return h.ElectionProof.VRFProof != nil
}

// IsEpochBeyondCurrMax is used in Filcns to detect delayed blocks.
// We are currently using defaults here and not worrying about it.
// We will consider potential changes of Consensus interface in https://github.com/filecoin-project/eudico/issues/143.
func (bft *Mir) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	return false
}

// Weight in mir uses a default approach where the height determines the weight.
//
// Every tipset in mir has a single block.
func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	return big.NewInt(int64(ts.Height() + 1)), nil
}
