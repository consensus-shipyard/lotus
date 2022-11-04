package kit

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/sync/errgroup"

	lapi "github.com/filecoin-project/lotus/api"
)

const (
	testTimeout = 1200
)

// SubnetHeightCheckForBlocks checks that `n` blocks with correct heights in the subnet will be mined.
// TODO: Ideally we should wait for a specific epoch/height of a block to see that all nodes are
// able to sync and mine up till there.
// A way of doing it may be to add a ChainHead listener that blocks until the height is reached
// in an independent go routine for each node
func SubnetHeightCheckForBlocks(ctx context.Context, n int, api lapi.FullNode) error {
	heads, err := api.ChainNotify(ctx)
	if err != nil {
		return err
	}

	var currHead []*lapi.HeadChange

	select {
	case <-ctx.Done():
		return fmt.Errorf("closed channel")
	case currHead = <-heads:
	}

	i := 0
	for i < n {
		select {
		case <-ctx.Done():
			return fmt.Errorf("closed channel")
		case newHead := <-heads:
			newHead[0].Val.Height()
			if newHead[0].Val.Height() <= currHead[0].Val.Height() {
				return fmt.Errorf("wrong %d block height: prev block height - %d, current head height - %d",
					i, currHead[0].Val.Height(), newHead[0].Val.Height())
			}

			currHead = newHead
			i++
		}
	}

	return nil
}

func SubnetHeightCheck(ctx context.Context, n int, nodes ...*TestFullNode) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, node := range nodes {
		node := node
		g.Go(func() error {
			if err := SubnetHeightCheckForBlocks(ctx, n, node); err != nil {
				return err
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	return nil
}

// WaitForMessageWithAvailable is a wrapper on StateWaitMsg that looks back up to limit epochs in the chain for a message.
//
// We need to wrap `StateWaitMsg` in this function in case the `GetCMessage` inside `StateWaitMsg` fails.
// In Mir this can be the case. A node may not receive a message until it received the validated batch
// (because the consensus goes so fast) so it doesn't have the message yet in its local ChainStore and
// `StateWaitMsg` fails. This wrapper in `strict=false` disregards errors from `StateWaitMsg` for a
// specific timeout.
func WaitForMessageWithAvailable(ctx context.Context, n *TestFullNode, c cid.Cid, strict bool) error {
	for {
		select {
		case <-time.After(testTimeout * time.Second):
			return fmt.Errorf("WaitForMessageWithAvailable timeout expired")
		default:

		}
		_, err := n.StateWaitMsg(ctx, c, 5, 100, true)
		if err != nil {
			if !strict {
				continue
			}
			return err
		}
		if err == nil {
			return nil
		}
	}
}

func MirNodesWaitMsg(ctx context.Context, msg cid.Cid, nodes ...*TestFullNode) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, node := range nodes {
		node := node
		g.Go(func() error {
			if err := WaitForMessageWithAvailable(ctx, node, msg, false); err != nil {
				return err
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

func NodeLibp2pAddr(h host.Host) (m multiaddr.Multiaddr, err error) {
	peerInfo := peer.AddrInfo{
		ID:    h.ID(),
		Addrs: []multiaddr.Multiaddr{h.Addrs()[0]},
	}

	addrs, err := peer.AddrInfoToP2pAddrs(&peerInfo)
	if err != nil {
		return nil, err
	}

	return addrs[0], nil
}

func RandomDelay(seconds int) {
	rand.Seed(time.Now().UnixNano())
	time.Sleep(time.Duration(rand.Intn(seconds)) * time.Second)
}
