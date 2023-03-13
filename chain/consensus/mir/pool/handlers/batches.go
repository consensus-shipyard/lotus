package handlers

import (
	"github.com/filecoin-project/mir/pkg/dsl"
	mpdsl "github.com/filecoin-project/mir/pkg/pb/mempoolpb/dsl"
	mempoolpb "github.com/filecoin-project/mir/pkg/pb/mempoolpb/types"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
	requestpbtypes "github.com/filecoin-project/mir/pkg/pb/requestpb/types"
	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/types"
)

type requestTxIDsContext struct {
	txs    []*requestpbtypes.Request
	origin *mempoolpb.RequestBatchOrigin
}

// IncludeBatchCreation registers event handlers for processing NewRequests and RequestBatch events.
func IncludeBatchCreation(
	m dsl.Module,
	mc *types.ModuleConfig,
	_ *types.ModuleParams,
	state *types.State,
) {
	mpdsl.UponTransactionIDsResponse(m, func(txIDs []t.TxID, context *requestTxIDsContext) error {
		var txs []*requestpbtypes.Request
		for i := range txIDs {
			tx := context.txs[i]
			txs = append(txs, tx)
		}
		mpdsl.NewBatch(m, context.origin.Module, txIDs, txs, context.origin)
		return nil
	})

	mpdsl.UponRequestBatch(m, func(origin *mempoolpb.RequestBatchOrigin) error {
		inputChan := make(chan []*mirproto.Request)
		state.ReadyForTxsChan <- inputChan
		var txs []*requestpbtypes.Request

		for _, r := range <-inputChan {
			tx := requestpbtypes.RequestFromPb(r)
			txs = append(txs, tx)
		}
		mpdsl.RequestTransactionIDs(m, mc.Self, txs, &requestTxIDsContext{txs, origin})
		return nil
	})
}
