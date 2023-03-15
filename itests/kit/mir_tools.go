package kit

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/sync/errgroup"

	"github.com/consensus-shipyard/go-ipc-types/validator"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
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

// CheckNodesInSync checks that all the synced nodes are in sync up with the base node till its current
// height, if for some reason any of the nodes haven't seen a block
// for certain height yet, the check waits up to a timeout to see if
// the node not synced receives the block for that height, and if this
// is not the case it returns an error.
//
// NOTE: This function takes as the base for the comparison the tipset for the base node,
// assuming the base node has the most up-to-date chain (we can probably make this configurable).
func CheckNodesInSync(ctx context.Context, from abi.ChainEpoch, baseNode *TestFullNode, checkedNodes ...*TestFullNode) error {
	_, err := baseNode.IsSyncedWith(ctx, from, checkedNodes...)
	return err
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
			base, err := ChainHeadWithCtx(ctx, node)
			if err != nil {
				return err
			}
			return mir.WaitForHeight(ctx, base.Height()+abi.ChainEpoch(blocks), node)
		})
	}
	return g.Wait()
}

// WaitForMessageWithAvailable is a wrapper on StateWaitMsg that looks back up to limit epochs in the chain for a message.
//
// We need to wrap `StateWaitMsg` in this function in case the `GetCMessage` inside `StateWaitMsg` fails.
// In Mir this can be the case. A node may not receive a message until it received the validated batch
// (because the consensus goes so fast) so it doesn't have the message yet in its local ChainStore and
// `StateWaitMsg` fails. This wrapper in `strict=false` disregards errors from `StateWaitMsg` for a
// specific timeout.
func WaitForMessageWithAvailable(ctx context.Context, n api.FullNode, c cid.Cid, strict bool) error {
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

func WaitForMsg(ctx context.Context, msg cid.Cid, nodes ...api.FullNode) error {
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

func PutValueToMirDB(ctx context.Context, t *testing.T, miners []*TestValidator) error {
	for _, m := range miners {
		err := m.GetDB().Put(ctx, ds.NewKey(t.Name()), []byte(t.Name()))
		if err != nil {
			return err
		}
	}
	return nil
}

func GetEmptyValueFromMirDB(ctx context.Context, t *testing.T, miners []*TestValidator) error {
	for _, m := range miners {
		_, err := m.GetDB().Get(ctx, ds.NewKey(t.Name()))
		if err != ds.ErrNotFound {
			return err
		}
		if err == nil {
			return fmt.Errorf("got non empty value")
		}
	}
	return nil
}

func GetNonEmptyValueFromMirDB(ctx context.Context, t *testing.T, miners []*TestValidator) error {
	for _, m := range miners {
		v, err := m.GetDB().Get(ctx, ds.NewKey(t.Name()))
		if err != nil {
			return err
		}
		if !bytes.Equal([]byte(t.Name()), v) {
			return fmt.Errorf("expected %v, got %v", t.Name(), v)
		}

	}
	return nil
}

func CountPeerIDs(peers []peer.AddrInfo) int {
	peerIDs := make(map[peer.ID]struct{})
	for _, p := range peers {
		peerIDs[p.ID] = struct{}{}
	}
	return len(peerIDs)
}
