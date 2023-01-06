package tspow

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/filecoin-project/lotus/lib/async"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/beacon"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/sigs"
)

const (
	MaxDiffLookback = 70

	// MaxHeightDrift is the epochs number to define blocks that will be rejected,
	// if there are more than MaxHeightDrift epochs above the theoretical max height
	// based on systime.
	MaxHeightDrift = 5
)

var (
	_   consensus.Consensus = &TSPoW{}
	log                     = logging.Logger("tspow-consensus")
)

type TSPoW struct {
	beacon  beacon.Schedule
	sm      *stmgr.StateManager
	genesis *types.TipSet
}

func NewTSPoWConsensus(
	sm *stmgr.StateManager,
	beacon beacon.Schedule,
	genesis chain.Genesis,
) *TSPoW {
	return &TSPoW{
		beacon:  beacon,
		sm:      sm,
		genesis: genesis,
	}
}

func (tsp *TSPoW) CreateBlock(ctx context.Context, w lapi.Wallet, bt *lapi.BlockTemplate) (*types.FullBlock, error) {
	pts, err := tsp.sm.ChainStore().LoadTipSet(ctx, bt.Parents)
	if err != nil {
		return nil, fmt.Errorf("failed to load parent tipset: %w", err)
	}

	next, blsMessages, secpkMessages, err := consensus.CreateBlockHeader(ctx, tsp.sm, pts, bt)
	if err != nil {
		return nil, xerrors.Errorf("failed to process messages from block template: %w", err)
	}

	tgt := big.Zero()
	tgt.SetBytes(next.Ticket.VRFProof)

	bestH := *next
	for i := 0; i < 10000; i++ {
		next.ElectionProof = &types.ElectionProof{
			VRFProof: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		}
		rand.Read(next.ElectionProof.VRFProof) //nolint:errcheck
		if work(&bestH).LessThan(work(next)) {
			bestH = *next
			if work(next).GreaterThanEqual(tgt) {
				break
			}
		}
	}
	next = &bestH

	if work(next).LessThan(tgt) {
		return nil, nil
	}

	if err := consensus.SignBlock(ctx, w, next.Miner, next); err != nil {
		return nil, xerrors.Errorf("failed to sign new block: %w", err)
	}

	return &types.FullBlock{
		Header:        next,
		BlsMessages:   blsMessages,
		SecpkMessages: secpkMessages,
	}, nil
}

func (tsp *TSPoW) ValidateBlock(ctx context.Context, b *types.FullBlock) (err error) {
	if err := blockSanityChecks(b.Header); err != nil {
		return fmt.Errorf("incoming header failed basic sanity checks: %w", err)
	}

	h := b.Header

	baseTs, err := tsp.sm.ChainStore().LoadTipSet(ctx, types.NewTipSetKey(h.Parents...))
	if err != nil {
		return fmt.Errorf("load parent tipset failed (%s): %w", h.Parents, err)
	}

	// fast checks first
	if h.Height != baseTs.Height()+1 {
		return fmt.Errorf("block height not parent height+1: %d != %d", h.Height, baseTs.Height()+1)
	}

	now := uint64(build.Clock.Now().Unix())
	if h.Timestamp > now+build.AllowableClockDriftSecs {
		return fmt.Errorf("block was from the future (now=%d, blk=%d): %w", now, h.Timestamp, consensus.ErrTemporal)
	}
	if h.Timestamp > now {
		log.Warn("Got block from the future, but within threshold", h.Timestamp, build.Clock.Now().Unix())
	}

	// check work above threshold
	w := work(b.Header)
	thr := big.Zero()
	thr.SetBytes(b.Header.Ticket.VRFProof)
	if thr.GreaterThan(w) {
		return fmt.Errorf("block below work threshold")
	}

	// check work threshold
	if b.Header.Height < MaxDiffLookback {
		if !thr.Equals(GenesisWorkTarget) {
			return fmt.Errorf("wrong work target")
		}
	} else {
		//
		lbr := b.Header.Height - DiffLookback(baseTs.Height())
		lbts, err := tsp.sm.ChainStore().GetTipsetByHeight(ctx, lbr, baseTs, false)
		if err != nil {
			return fmt.Errorf("failed to get lookback tipset+1: %w", err)
		}

		expDiff := Difficulty(baseTs, lbts)
		if !thr.Equals(expDiff) {
			return fmt.Errorf("expected adjusted difficulty %s, was %s (act-exp: %s)", expDiff, thr, big.Sub(thr, expDiff))
		}
	}

	pweight, err := Weight(context.TODO(), nil, baseTs)
	if err != nil {
		return fmt.Errorf("getting parent weight: %w", err)
	}

	if types.BigCmp(pweight, b.Header.ParentWeight) != 0 {
		return fmt.Errorf("parrent weight different: %s (header) != %s (computed)",
			b.Header.ParentWeight, pweight)
	}

	minerCheck := async.Err(func() error {
		if err := tsp.minerIsValid(h.Miner); err != nil {
			return fmt.Errorf("minerIsValid failed: %w", err)
		}
		return nil
	})

	commonChecks := consensus.CommonBlkChecks(ctx, tsp.sm, tsp.sm.ChainStore(), b, baseTs)
	await := append([]async.ErrorFuture{
		minerCheck,
	}, commonChecks...)

	return consensus.RunAsyncChecks(ctx, await)
}

func (tsp *TSPoW) IsEpochBeyondCurrMax(epoch abi.ChainEpoch) bool {
	if tsp.genesis == nil {
		return false
	}

	now := uint64(build.Clock.Now().Unix())
	return epoch > (abi.ChainEpoch((now-tsp.genesis.MinTimestamp())/build.BlockDelaySecs) + MaxHeightDrift)
}

func (tsp *TSPoW) minerIsValid(maddr address.Address) error {
	switch maddr.Protocol() {
	case address.BLS:
		fallthrough
	case address.SECP256K1:
		return nil
	}

	return fmt.Errorf("miner address must be a key")
}

func (tsp *TSPoW) ValidateBlockHeader(ctx context.Context, b *types.BlockHeader) (rejectReason string, err error) {
	if err := tsp.minerIsValid(b.Miner); err != nil {
		return err.Error(), err
	}

	err = sigs.CheckBlockSignature(ctx, b, b.Miner)
	if err != nil {
		log.Errorf("block signature verification failed: %s", err)
		return "signature_verification_failed", err
	}

	return "", nil
}

func blockSanityChecks(h *types.BlockHeader) error {
	if h.Ticket == nil {
		return xerrors.Errorf("block must not have nil ticket")
	}
	if h.BlockSig == nil {
		return xerrors.Errorf("block had nil signature")
	}

	if h.BLSAggregate == nil {
		return xerrors.Errorf("block had nil bls aggregate signature")
	}

	if h.Miner.Protocol() != address.SECP256K1 {
		return xerrors.Errorf("block had non-secp miner address")
	}

	if len(h.Parents) != 1 {
		return xerrors.Errorf("must have 1 parent")
	}

	return nil
}
