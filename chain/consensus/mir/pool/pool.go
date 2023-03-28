// Package pool maintains the request pool used to send requests from Lotus's mempool to Mir.
package pool

import (
	"context"
	"fmt"

	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	requestpbtypes "github.com/filecoin-project/mir/pkg/pb/requestpb/types"
)

type Fetcher struct {
	ReadyForTxsChan chan chan []*requestpb.Request
	ctx             context.Context
}

func NewFetcher(ctx context.Context, ch chan chan []*requestpb.Request) *Fetcher {
	return &Fetcher{
		ctx:             ctx,
		ReadyForTxsChan: ch,
	}
}

func (f *Fetcher) Fetch() []*requestpbtypes.Request {
	inputChan := make(chan []*requestpb.Request)
	f.ReadyForTxsChan <- inputChan

	var txs []*requestpbtypes.Request

	select {
	case <-f.ctx.Done():
		fmt.Println(">>> fetch context closed")
		return nil
	case input := <-inputChan:
		for _, r := range input {
			tx := requestpbtypes.RequestFromPb(r)
			txs = append(txs, tx)
		}
		return txs
	}
}
