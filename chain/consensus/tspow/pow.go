package tspow

import (
	"context"
	bignumbers "math/big"
	"sort"

	"github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/specs-actors/v8/actors/builtin"

	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
)

// TODO: Consider moving this to build params and differentiate
// between testing (low diff) and deployment (high diff).
const GenesisPoWTarget = "2019783675352289407433363"

// const GenesisPoWTarget = "4519783675352289407433363"

var RewardFunc = func(ctx context.Context, vmi vm.Interface, em stmgr.ExecMonitor,
	epoch abi.ChainEpoch, ts *types.TipSet, params *reward.AwardBlockRewardParams) error {
	rwMsg := &types.Message{
		From:       builtin.RewardActorAddr,
		To:         params.Miner,
		Nonce:      uint64(epoch),
		Value:      abi.NewTokenAmount(1), // NOTE: Fixed reward of one FIL for PoW
		GasFeeCap:  types.NewInt(0),
		GasPremium: types.NewInt(0),
		GasLimit:   1 << 30,
		Method:     0,
	}
	ret, actErr := vmi.ApplyImplicitMessage(ctx, rwMsg)
	if actErr != nil {
		return xerrors.Errorf("failed to apply reward message for miner %s: %w", params.Miner, actErr)
	}
	if em != nil {
		if err := em.MessageApplied(ctx, ts, rwMsg.Cid(), rwMsg, ret, true); err != nil {
			return xerrors.Errorf("callback failed on reward message: %w", err)
		}
	}

	if ret.ExitCode != 0 {
		return xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
	}
	return nil
}

func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	if ts == nil {
		return types.NewInt(0), nil
	}

	w := ts.ParentWeight()
	for _, header := range ts.Blocks() {
		w = big.Add(w, work(header))
	}

	return w, nil
}

func work(bh *types.BlockHeader) big.Int {
	w := big.NewInt(0)
	w.SetBytes([]byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	})

	bhc := *bh
	bhc.BlockSig = nil

	dmh, err := multihash.Decode(bhc.Cid().Hash())
	if err != nil {
		panic(err) // todo probably definitely not a good idea
	}
	s := big.NewInt(0)
	s.SetBytes(dmh.Digest)
	return big.Div(w, s)
}

func DiffLookback(baseH abi.ChainEpoch) abi.ChainEpoch {
	lb := ((baseH + 11) * 217) % (MaxDiffLookback - 40)
	return lb + 40
}

func Difficulty(baseTs, lbts *types.TipSet) big.Int {
	expLbTime := 100000 * uint64(DiffLookback(baseTs.Height())) * build.BlockDelaySecs
	actTime := (baseTs.Blocks()[0].Timestamp - lbts.Blocks()[0].Timestamp) * 100000

	actTime = expLbTime - uint64(int64(expLbTime-actTime)/100)

	// clamp max adjustment
	if actTime < expLbTime*99/100 {
		actTime = expLbTime * 99 / 100
	}
	if actTime > expLbTime*101/100 {
		actTime = expLbTime * 101 / 100
	}

	prevdiff := big.Zero()
	prevdiff.SetBytes(baseTs.Blocks()[0].Ticket.VRFProof)
	diff := big.Div(types.BigMul(prevdiff, big.NewInt(int64(expLbTime))), big.NewInt(int64(actTime)))

	pgen, _ := bignumbers.NewFloat(0).SetInt(GenesisWorkTarget.Int).Float64()
	fdiff, _ := bignumbers.NewFloat(0).SetInt(diff.Int).Float64()
	pgen = fdiff * 100 / pgen
	// Difficulty adjustement print. Really helpful for debugging purposes.
	log.Debugf("adjust %.4f%%, p%s lb%d (%.4f%% gen)\n", 100*float64(expLbTime)/float64(actTime), prevdiff, DiffLookback(baseTs.Height()), pgen)

	return diff
}

func BestWorkBlock(ts *types.TipSet) *types.BlockHeader {
	blks := ts.Blocks()
	sort.Slice(blks, func(i, j int) bool {
		return work(blks[i]).GreaterThan(work(blks[j]))
	})
	return blks[0]
}

var GenesisWorkTarget = func() big.Int {
	w, _ := big.FromString(GenesisPoWTarget)
	return w
}()
