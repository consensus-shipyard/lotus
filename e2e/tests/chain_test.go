package tests

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestMain(m *testing.M) {
	for _, id := range []string{"0", "1", "2", "3"} {
		err := waitForAuthToken(id)
		if err != nil {
			panic(err)
		}
		err = waitForLotusAPI(id)
		if err != nil {
			panic(err)
		}
	}
	m.Run()
}

// TestMirSmoke_ConnectNodes tests that nodes can be connected with each other.
func TestMirSmoke_ConnectNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nodes := ClientsFor(ctx, t, "0", "1", "2", "3")
	var peerInfo []peer.AddrInfo

	for i, n := range nodes {

		addr, err := n.NetAddrsListen(ctx)
		require.NoError(t, err)

		peerInfo = append(peerInfo, addr)
		t.Logf("node %d address: %v", i, addr)
	}

	for i, n := range nodes {
		for j, info := range peerInfo {
			p := peer.AddrInfo{info.ID, []multiaddr.Multiaddr{info.Addrs[0]}}

			err := n.NetConnect(ctx, p)
			if i != j {
				require.NoError(t, err)
			}
		}
	}

	for i, n := range nodes {
		peers, err := n.NetPeers(ctx)
		require.NoError(t, err)

		t.Logf("node %v connected to: %v\n", i, peers)

		require.Equal(t, len(nodes)-1, len(peers))
	}
}

func TestMirSmoke_AllNodesMine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		err := g.Wait()
		require.NoError(t, err)
		t.Logf("[*] defer: system %s stopped", t.Name())
	}()

	nodes := ClientsFor(ctx, t, "0", "1", "2", "3")

	err := waitForHeight(ctx, 20, nodes...)
	require.NoError(t, err)
}
