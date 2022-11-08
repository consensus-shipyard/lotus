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

	"github.com/filecoin-project/go-state-types/abi"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	testTimeout = 1200
)

// CheckNodesInSync checks that all the synced nodes are in sync up with the base node till its current
// height, if for some reason any of the nodes haven't seen a block
// for certain height yet, the check waits up to a timeout to see if
// the node not synced receives the block for that height, and if this
// is not the case it returns an error.
//
// NOTE: This function takes as the base for the comparison the tipset for the base node,
// assuming the base node has the most up-to-date chain (we can probably make this configurable).
func CheckNodesInSync(ctx context.Context, from abi.ChainEpoch, base *TestFullNode, synced ...*TestFullNode) error {
	if len(synced) < 1 {
		return fmt.Errorf("no nodes are specified")
	}
	tip, err := base.ChainHead(ctx)
	if err != nil {
		return err
	}
	height := tip.Height()
	for from <= height {
		baseTipSet, err := base.ChainGetTipSetByHeight(ctx, from, types.EmptyTSK)
		if err != nil {
			return err
		}
		if baseTipSet.Height() != from {
			return fmt.Errorf("couldn't find tipset for height in base node")
		}

		// TODO: We can probably parallelize the check for each node?
		for ni, node := range synced {
			// 20 seconds baseline timeout
			timeout := 20 * time.Second
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			// wait for tipset in height (if it arrives)
			errCh := make(chan error, 1)
			go func(node *TestFullNode) {
				i := from
				for {
					ts, err := node.ChainGetTipSetByHeight(ctx, i, types.EmptyTSK)
					if err != nil {
						// errCh <- err
						// disregard these errors, you can optionally print them.
						fmt.Printf("ERROR GETTING TIPSET BY HEIGHT IN NODE: %d: %v", ni, err)
						continue
					}
					if ts.Height() < baseTipSet.Height() {
						// we are not synced yet, so continue
						time.Sleep(500 * time.Second)
						continue
					}
					if ts.Height() != baseTipSet.Height() {
						errCh <- fmt.Errorf("something went wrong. we didn´t reach the same height in node %d", i)
						return
					}
					if ts.Key() != baseTipSet.Key() {
						errCh <- fmt.Errorf("something went wrong. we didn´t reach the same cid in node %d", i)
						return
					}
					errCh <- nil
					return
				}
			}(node)

			select {
			case err := <-errCh:
				if err != nil {
					return nil
				}
			case <-ctx.Done():
				return fmt.Errorf("block for height not found after deadline")
			}
			from++
		}
	}

	return nil
}

func ChainHeightCheckForBlocks(ctx context.Context, n int, api lapi.FullNode) error {
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

// ChainHeightCheck verifies that an amount of arbitrary blocks was added to
// the chain. This check is used to ensure that the chain keeps advances but
// performs no deeper check into the blocks created. We don't check if all
// nodes created the same blocks for the same height. If you need to perform deeper
// checks (for instance to see if the nodes have forked) you should use some other
// check.
func ChainHeightCheck(ctx context.Context, blocks int, nodes ...*TestFullNode) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, node := range nodes {
		node := node
		g.Go(func() error {
			if err := ChainHeightCheckForBlocks(ctx, blocks, node); err != nil {
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

func ChainHeightCheckWithFaultyNodes(ctx context.Context, blocks int, nodes []*TestFullNode, faultyNodes ...*TestFullNode) error {
	oldHeights := make([]abi.ChainEpoch, len(faultyNodes))
	newHeights := make([]abi.ChainEpoch, len(faultyNodes))

	// Adding an initial buffer for peers to sync their chain head.
	time.Sleep(500 * time.Millisecond)

	for i, fn := range faultyNodes {
		ts, err := fn.FullNode.ChainHead(ctx)
		if err != nil {
			return err
		}
		if ts == nil {
			return fmt.Errorf("nil tipset for an old block")
		}
		oldHeights[i] = ts.Height()
	}

	err := ChainHeightCheck(ctx, blocks, nodes...)
	if err != nil {
		return err
	}

	for i, fn := range faultyNodes {
		ts, err := fn.FullNode.ChainHead(ctx)
		if err != nil {
			return err
		}
		if ts == nil {
			return fmt.Errorf("nil tipset for an new block")
		}
		newHeights[i] = ts.Height()
	}

	for i := range newHeights {
		h1 := newHeights[i]
		h2 := oldHeights[i]
		if h1 != h2 {
			return fmt.Errorf("different heights for miner %d: new - %d, old - %d", i, h1, h2)
		}
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
