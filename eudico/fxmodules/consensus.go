package fxmodules

import (
	"go.uber.org/fx"

	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
)

func Consensus(algorithm consensus.Algorithm) fx.Option {
	switch algorithm {
	case consensus.Expected:
		return filecoinExpectedConsensusModule
	case consensus.Mir:
		return mirConsensusModule
	case consensus.TSPoW:
		return tspowConsensusModule
	default:
		panic("Unsupported consensus algorithm")
	}
}

var filecoinExpectedConsensusModule = fx.Module("filecoinExpectedConsensus",
	fx.Provide(filcns.NewFilecoinExpectedConsensus),
	fx.Supply(store.WeightFunc(filcns.Weight)),
	fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(filcns.RewardFunc), fx.As(new(stmgr.Executor)))),
)

var mirConsensusModule = fx.Module("mirConsensus",
	fx.Provide(fx.Annotate(mir.NewConsensus, fx.As(new(consensus.Consensus)))),
	fx.Supply(store.WeightFunc(mir.Weight)),
	fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(mir.RewardFunc), fx.As(new(stmgr.Executor)))),
)

var tspowConsensusModule = fx.Module("tspowConsensus",
	fx.Provide(fx.Annotate(tspow.NewConsensus, fx.As(new(consensus.Consensus)))),
	fx.Supply(store.WeightFunc(tspow.Weight)),
	fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(tspow.RewardFunc), fx.As(new(stmgr.Executor)))),
)
