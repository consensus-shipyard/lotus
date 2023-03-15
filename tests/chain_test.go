package tests

import (
	"context"
	"io/ioutil"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

func getAuthToken(id string) (string, error) {
	b, err := ioutil.ReadFile("../_data/" + id + "/token")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func getAuthToken2(id string) (string, error) {
	b, err := ioutil.ReadFile("/Users/alpha/.lotus-local-net" + id + "/token")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func ClientsFor(ctx context.Context, t *testing.T, ids ...string) (clients []api.FullNode) {
	for _, id := range ids {
		clients = append(clients, ClientFor(ctx, t, id))
	}
	return clients
}

func ClientFor(ctx context.Context, t *testing.T, id string) api.FullNode {
	token, err := getAuthToken2(id)
	require.NoError(t, err)

	headers := http.Header{"Authorization": []string{"Bearer " + string(token)}}

	c, closer, err := client.NewFullNodeRPCV1(ctx, "ws://127.0.0.1:123"+id+"/rpc/v1", headers)
	require.NoError(t, err)

	t.Cleanup(func() {
		closer()
	})

	return c
}

func Test_ChainGrows(t *testing.T) {
	ctx := context.Background()

	c := ClientFor(ctx, t, "0")

	head, err := c.ChainHead(ctx)
	require.NoError(t, err)

	require.Greater(t, head.Height(), abi.ChainEpoch(1))
}

func waitForHeight(ctx context.Context, height abi.ChainEpoch, clients ...v1api.FullNode) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, node := range clients {
		node := node
		g.Go(func() error {
			err := mir.WaitForHeight(ctx, height, node)
			if err != nil {
				return err
			}
			return nil
		})
	}

	return g.Wait()
}

func TestMirBasic_AllNodesMiningWithMessaging(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		err := g.Wait()
		require.NoError(t, err)
		t.Logf("[*] defer: system %s stopped", t.Name())
	}()

	nodes := ClientsFor(ctx, t, "0", "1")

	err := waitForHeight(ctx, 10, nodes...)
	require.NoError(t, err)

	var cids []cid.Cid
	for range nodes {
		rand.Seed(time.Now().UnixNano())
		j := rand.Intn(len(nodes))
		src, err := nodes[j].WalletDefaultAddress(ctx)
		require.NoError(t, err)

		dst, err := nodes[(j+1)%len(nodes)].WalletDefaultAddress(ctx)
		require.NoError(t, err)

		t.Logf(">>> node %s is sending a message to node %s", src, dst)

		smsg, err := nodes[j].MpoolPushMessage(ctx, &types.Message{
			From:  src,
			To:    dst,
			Value: big.Zero(),
		}, nil)
		require.NoError(t, err)
		cids = append(cids, smsg.Cid())
	}

	err = waitForHeight(ctx, 10, nodes...)
	require.NoError(t, err)

	for _, id := range cids {
		err = kit.WaitForMsg(ctx, id, nodes[0])
		require.NoError(t, err)
	}
}

func WaitForMsg(ctx context.Context, msg cid.Cid, nodes ...api.FullNode) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, node := range nodes {
		node := node
		g.Go(func() error {
			if err := kit.WaitForMessageWithAvailable(ctx, node, msg, false); err != nil {
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
