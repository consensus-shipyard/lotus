// Package pool maintains the request pool used to send requests from Lotus's mempool to Mir.
package pool

import (
	"context"

	mirproto "github.com/filecoin-project/mir/pkg/pb/trantorpb/types"
)

type Fetcher struct {
	ctx             context.Context
	ReadyForTxsChan chan chan []*mirproto.Transaction
}

func NewFetcher(ctx context.Context, ch chan chan []*mirproto.Transaction) *Fetcher {
	return &Fetcher{
		ctx:             ctx,
		ReadyForTxsChan: ch,
	}
}

func (f *Fetcher) Fetch() []*mirproto.Transaction {
	inputChan := make(chan []*mirproto.Transaction)
	select {
	case <-f.ctx.Done():
		return nil
	case f.ReadyForTxsChan <- inputChan:
	}

	var txs []*mirproto.Transaction

	select {
	case <-f.ctx.Done():
		return nil
	case input := <-inputChan:
		for _, r := range input {
			txs = append(txs, r)
		}
		return txs
	}
}
