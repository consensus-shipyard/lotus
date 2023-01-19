package kit

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	testTimeout = 1200
)

// TempFileName generates a temporary filename for use in testing or whatever
func TempFileName(suffix string) string {
	randBytes := make([]byte, 8)
	rand.Read(randBytes)
	return suffix + "_" + hex.EncodeToString(randBytes) + ".tmp"
}

// CheckNodesInSync checks that all the synced nodes are in sync up with the base node till its current
// height, if for some reason any of the nodes haven't seen a block
// for certain height yet, the check waits up to a timeout to see if
// the node not synced receives the block for that height, and if this
// is not the case it returns an error.
//
// NOTE: This function takes as the base for the comparison the tipset for the base node,
// assuming the base node has the most up-to-date chain (we can probably make this configurable).
func CheckNodesInSync(ctx context.Context, from abi.ChainEpoch, baseNode *TestFullNode, checkedNodes ...*TestFullNode) error {
	if len(checkedNodes) < 1 {
		return fmt.Errorf("no checked nodes")
	}
	baseHead, err := baseNode.ChainHead(ctx)
	if err != nil {
		return err
	}

	for h := from; h <= baseHead.Height(); h++ {
		h := h
		baseTipSet, err := baseNode.ChainGetTipSetByHeight(ctx, h, types.EmptyTSK)
		if err != nil {
			return err
		}
		if baseTipSet.Height() != h {
			return fmt.Errorf("couldn't find tipset for height %d in base node", h)
		}

		// TODO: We can probably parallelize the check for each node?

		g, ctx := errgroup.WithContext(ctx)

		for _, node := range checkedNodes {
			node := node
			// We don't need to check that base node is in sync with itself.
			if node == baseNode {
				continue
			}
			g.Go(func() error {
				return waitNodeInSync(ctx, h, baseTipSet, node)
			})
		}

		if err := g.Wait(); err != nil {
			return err
		}
	}
	return nil
}

// waitNodeInSync waits when the tipset at height will be equal to targetTipSet value.
func waitNodeInSync(ctx context.Context, height abi.ChainEpoch, targetTipSet *types.TipSet, node *TestFullNode) error {
	// one minute baseline timeout
	timeout := 10 * time.Second
	base, err := node.ChainHead(ctx)
	if err != nil {
		return err
	}
	if base.Height() < height {
		timeout = timeout + time.Duration(height-base.Height())*time.Second
	}
	after := time.After(timeout)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled: failed to find tipset in node")
		case <-after:
			return fmt.Errorf("timeout: failed to find tipset in node")
		default:
			ts, err := node.ChainGetTipSetByHeight(ctx, height, types.EmptyTSK)
			if err != nil {
				time.Sleep(1 * time.Second)
				continue
			}
			if ts.Height() < targetTipSet.Height() {
				// we are not synced yet, so continue
				time.Sleep(1 * time.Second)
				continue
			}
			if ts.Height() != targetTipSet.Height() {
				return fmt.Errorf("failed to reach the same height in node")
			}
			if ts.Key() != targetTipSet.Key() {
				return fmt.Errorf("failed to reach the same CID in node")
			}
			return nil
		}
	}
}

func WaitForBlock(ctx context.Context, height abi.ChainEpoch, api lapi.FullNode) error {
	// get base to determine the gap to sync and configure timeout.
	base, err := api.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("failed to get chain head: %w", err)
	}

	// one minute baseline timeout
	timeout := 60 * time.Second
	// add extra if the gap is big.
	if base.Height() < height {
		timeout = timeout + time.Duration(height-base.Height())*time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		head := abi.ChainEpoch(0)
		// poll until we get the desired height.
		// TODO: We may be able to add a slight sleep here if needed.
		for head != height {
			base, err := api.ChainHead(ctx)
			if err != nil {
				return err
			}
			head = base.Height()
			if head > height {
				break
			}
		}
		return nil
	})

	return g.Wait()
}

func ChainHeightCheckForBlocks(ctx context.Context, n int, api lapi.FullNode) error {
	base, err := api.ChainHead(ctx)
	if err != nil {
		return err
	}
	err = WaitForBlock(ctx, base.Height()+abi.ChainEpoch(n), api)
	if err != nil {
		return err
	}
	return nil
}

// AdvanceChain advances the chain and verifies that an amount of arbitrary blocks was added to
// the chain. This check is used to ensure that the chain keeps advances but
// performs no deeper check into the blocks created. We don't check if all
// nodes created the same blocks for the same height. If you need to perform deeper
// checks (for instance to see if the nodes have forked) you should use some other
// check.
func AdvanceChain(ctx context.Context, blocks int, nodes ...*TestFullNode) error {
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

	return g.Wait()
}

func ChainHeightCheckWithFaultyNodes(ctx context.Context, blocks int, nodes []*TestFullNode, faultyNodes ...*TestFullNode) error {
	oldHeights := make([]abi.ChainEpoch, len(faultyNodes))

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

	err := AdvanceChain(ctx, blocks, nodes...)
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
		newHeight := ts.Height()
		if newHeight != oldHeights[i] {
			return fmt.Errorf("different heights for miner %d: new - %d, old - %d", i, newHeight, oldHeights[i])
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
