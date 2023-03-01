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

	"github.com/filecoin-project/go-state-types/abi"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/consensus/mir/validator"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	MessageWaitTimeout = 1200 * time.Second
)

// TempFileName generates a temporary filename for use in testing or whatever
func TempFileName(suffix string) string {
	randBytes := make([]byte, 8)
	rand.Read(randBytes)
	return suffix + "_" + hex.EncodeToString(randBytes) + ".json"
}

func CheckNodesInSyncWithNextHeight(ctx context.Context, from abi.ChainEpoch, baseNode *TestFullNode, checkedNodes ...*TestFullNode) (abi.ChainEpoch, error) {
	if len(checkedNodes) < 1 {
		return 0, fmt.Errorf("no checked nodes")
	}
	base, err := ChainHeadWithCtx(ctx, baseNode)
	if err != nil {
		return 0, err
	}

	var tss []*types.TipSet
	to := base.Height()

	for h := from; h <= to; h++ {
		h := h

		baseTipSet, err := baseNode.ChainGetTipSetByHeight(ctx, h, types.EmptyTSK)
		if err != nil {
			return 0, err
		}
		if baseTipSet.Height() != h {
			return 0, fmt.Errorf("couldn't find tipset for height %d in base node", h)
		}

		tss = append(tss, baseTipSet)

		// TODO: We can probably parallelize the check for each node?

		g, ctx := errgroup.WithContext(ctx)

		for _, node := range checkedNodes {
			node := node

			// We don't need to check that base node is in sync with itself.
			if node == baseNode {
				continue
			}
			g.Go(func() error {
				return waitForTipSet(ctx, h, baseTipSet, node)
			})
		}

		if err := g.Wait(); err != nil {
			return 0, err
		}
	}

	for _, node := range checkedNodes {
		ts, err := node.ChainGetTipSetByHeight(ctx, to, types.EmptyTSK)
		if err != nil {
			return 0, err
		}
		tss = append(tss, ts)
	}

	fmt.Println(">>>>> CheckNodesInSync artifacts:", tss)
	return to, nil
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
	_, err := CheckNodesInSyncWithNextHeight(ctx, from, baseNode, checkedNodes...)
	return err
}

func waitForTipSet(ctx context.Context, height abi.ChainEpoch, targetTipSet *types.TipSet, node *TestFullNode) error {
	// one minute baseline timeout
	timeout := 10 * time.Second
	base, err := ChainHeadWithCtx(ctx, node)
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

func ChainHeightCheckForBlocks(ctx context.Context, n int, api lapi.FullNode) error {
	base, err := ChainHeadWithCtx(ctx, api)
	if err != nil {
		return err
	}
	return mir.WaitForHeight(ctx, base.Height()+abi.ChainEpoch(n), api)
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

// NoProgressForFaultyNodes checks that the heights of the faulty nodes are not changed after advancing the chain.
func NoProgressForFaultyNodes(ctx context.Context, blocks int, nodes []*TestFullNode, faultyNodes ...*TestFullNode) error {
	oldHeights := make([]abi.ChainEpoch, len(faultyNodes))

	time.Sleep(1 * time.Second)

	for i, fn := range faultyNodes {
		ts, err := ChainHeadWithCtx(ctx, fn.FullNode)
		if err != nil {
			return err
		}
		oldHeights[i] = ts.Height()
	}

	err := AdvanceChain(ctx, blocks, nodes...)
	if err != nil {
		return err
	}

	for i, fn := range faultyNodes {
		ts, err := ChainHeadWithCtx(ctx, fn.FullNode)
		if err != nil {
			return err
		}
		newHeight := ts.Height()
		if newHeight != oldHeights[i] {
			return fmt.Errorf("validator %d chain height updated: new - %d, old - %d", i, newHeight, oldHeights[i])
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
	after := time.After(MessageWaitTimeout)
	for {
		select {
		case <-after:
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

type fakeMembership struct {
}

func (f fakeMembership) GetValidatorSet() (*validator.Set, error) {
	return nil, fmt.Errorf("no validators")
}

func ChainHeadWithCtx(ctx context.Context, api v1api.FullNode) (*types.TipSet, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return api.ChainHead(ctx)
}
