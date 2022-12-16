package tspow

import (
	"context"
	"github.com/filecoin-project/go-state-types/abi"
	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
)

func Weight(ctx context.Context, stateBs bstore.Blockstore, ts *types.TipSet) (types.BigInt, error) {
	panic("Weight not implemented")
}

var RewardFunc = func(ctx context.Context, vmi vm.Interface, em stmgr.ExecMonitor,
	epoch abi.ChainEpoch, ts *types.TipSet, params *reward.AwardBlockRewardParams) error {
	panic("RewardFunc not implemented")
}
