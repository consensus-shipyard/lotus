package fxmodules

import (
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/consensus/filcns"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/consensus/tspow"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"go.uber.org/fx"
)

type ConsensusAlgorithm int

const (
	ExpectedConsensus ConsensusAlgorithm = iota
	MirConsensus
	TSPoWConsensus
)

func Consensus(algorithm ConsensusAlgorithm) fx.Option {
	switch algorithm {
	case ExpectedConsensus:
		return filecoinExpectedConsensusModule
	case MirConsensus:
		return mirConsensusModule
	case TSPoWConsensus:
		return tspowModule
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

var tspowModule = fx.Module("tspowModule",
	fx.Provide(fx.Annotate(tspow.NewTSPoWConsensus), fx.As(new(consensus.Consensus))),
	fx.Supply(store.WeightFunc(tspow.Weight)),
	fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(tspow.RewardFunc), fx.As(new(stmgr.Executor)))),
)
