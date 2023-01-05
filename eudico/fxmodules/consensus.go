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

type ConsensusAlgorithm int

const (
	ExpectedConsensus ConsensusAlgorithm = iota
	MirConsensus
	TSPoWConsensus
)

func Consensus(algorithm ConsensusAlgorithm) fx.Option {
	module := fxCase(algorithm,
		map[ConsensusAlgorithm]fx.Option{
			ExpectedConsensus: filecoinExpectedConsensusModule,
			MirConsensus:      mirConsensusModule,
			TSPoWConsensus:    tspowModule,
		})
	if module == nil {
		panic("Unsupported consensus algorithm")
	}
	return module
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
