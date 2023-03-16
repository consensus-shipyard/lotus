// Package pool maintains the request pool used to send requests from Lotus's mempool to Mir.
package pool

import (
	"github.com/filecoin-project/mir/pkg/dsl"
	"github.com/filecoin-project/mir/pkg/modules"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	requestpbtypes "github.com/filecoin-project/mir/pkg/pb/requestpb/types"

	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/handlers"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/types"
)

// ModuleConfig sets the module ids. All replicas are expected to use identical module configurations.
type ModuleConfig = types.ModuleConfig

// ModuleParams sets the values for the parameters of an instance of the protocol.
// All replicas are expected to use identical module parameters.
type ModuleParams = types.ModuleParams

// DefaultModuleConfig returns a valid module config with default names for all modules.
func DefaultModuleConfig() *ModuleConfig {
	return &ModuleConfig{
		Self:   "mempool",
		Hasher: "hasher",
	}
}

// DefaultModuleParams returns a valid module config with default names for all modules.
func DefaultModuleParams() *ModuleParams {
	return &ModuleParams{
		MaxTransactionsInBatch: 512,
	}
}

// NewModule creates a new instance of a request pool module implementation.
func NewModule(ch chan chan []*requestpb.Request, mc *ModuleConfig, p *ModuleParams) modules.Module {
	m := dsl.NewModule(mc.Self)

	state := &types.State{
		ReadyForTxsChan: ch,
	}

	handlers.IncludeComputationOfTransactionAndBatchIDs(m, mc, p)
	handlers.IncludeBatchCreation(m, mc, p, state)

	return m
}

type Fetcher struct {
	ReadyForTxsChan chan chan []*requestpb.Request
}

func NewFetcher(ch chan chan []*requestpb.Request) *Fetcher {
	return &Fetcher{
		ReadyForTxsChan: ch,
	}
}

func (f *Fetcher) Fetch() []*requestpbtypes.Request {
	inputChan := make(chan []*requestpb.Request)
	f.ReadyForTxsChan <- inputChan
	var txs []*requestpbtypes.Request

	for _, r := range <-inputChan {
		tx := requestpbtypes.RequestFromPb(r)
		txs = append(txs, tx)
	}

	return txs
}
