package handlers

import (
	"github.com/filecoin-project/mir/pkg/dsl"
	"github.com/filecoin-project/mir/pkg/mempool/simplemempool/emptybatchid"
	mpdsl "github.com/filecoin-project/mir/pkg/pb/mempoolpb/dsl"
	mempoolpb "github.com/filecoin-project/mir/pkg/pb/mempoolpb/types"
	requestpbtypes "github.com/filecoin-project/mir/pkg/pb/requestpb/types"
	"github.com/filecoin-project/mir/pkg/serializing"
	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/types"
)

// IncludeComputationOfTransactionAndBatchIDs registers event handler for processing RequestTransactionIDs and
// RequestBatchID events.
func IncludeComputationOfTransactionAndBatchIDs(
	m dsl.Module,
	mc *types.ModuleConfig,
	_ *types.ModuleParams,
) {
	mpdsl.UponRequestTransactionIDs(m, func(txs []*requestpbtypes.Request, origin *mempoolpb.RequestTransactionIDsOrigin) error {
		txMsgs := make([][][]byte, len(txs))
		for i, tx := range txs {
			txMsgs[i] = serializing.RequestForHash(tx.Pb())
		}
		dsl.HashRequest(m, mc.Hasher, txMsgs, &computeHashForTransactionIDsContext{origin})
		return nil
	})

	dsl.UponHashResult(m, func(hashes [][]byte, context *computeHashForTransactionIDsContext) error {
		txIDs := make([]t.TxID, len(hashes))
		copy(txIDs, hashes)
		mpdsl.TransactionIDsResponse(m, context.origin.Module, txIDs, context.origin)
		return nil
	})

	mpdsl.UponRequestBatchID(m, func(txIDs []t.TxID, origin *mempoolpb.RequestBatchIDOrigin) error {
		data := make([][]byte, len(txIDs))
		copy(data, txIDs)
		if len(txIDs) == 0 {
			mpdsl.BatchIDResponse(m, origin.Module, emptybatchid.EmptyBatchID(), origin)
		}
		dsl.HashOneMessage(m, mc.Hasher, data, &computeHashForBatchIDContext{origin})
		return nil
	})

	dsl.UponOneHashResult(m, func(hash []byte, context *computeHashForBatchIDContext) error {
		mpdsl.BatchIDResponse(m, context.origin.Module, hash, context.origin)
		return nil
	})
}

// Context data structures

type computeHashForTransactionIDsContext struct {
	origin *mempoolpb.RequestTransactionIDsOrigin
}

type computeHashForBatchIDContext struct {
	origin *mempoolpb.RequestBatchIDOrigin
}
