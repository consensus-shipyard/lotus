// Package pool maintains the request pool used to send requests from Lotus's mempool to Mir.
package pool

import (
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	requestpbtypes "github.com/filecoin-project/mir/pkg/pb/requestpb/types"
)

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
