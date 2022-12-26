package mir

import (
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/store"
	"go.uber.org/fx"
)

var Module = fx.Module("mirConsensus",
	fx.Provide(fx.Annotate(NewConsensus, fx.As(new(consensus.Consensus)))),
	fx.Supply(store.WeightFunc(Weight)),
	fx.Supply(fx.Annotate(consensus.NewTipSetExecutor(RewardFunc), fx.As(new(stmgr.Executor)))),
)
