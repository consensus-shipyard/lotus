// Package tests contains end-to-end tests for Eudico nodes.
package tests

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
)

func getAuthToken(id string) (string, error) {
	b, err := ioutil.ReadFile("../_data/" + id + "/token")
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
	token, err := getAuthToken(id)
	require.NoError(t, err)

	headers := http.Header{"Authorization": []string{"Bearer " + string(token)}}

	c, closer, err := client.NewFullNodeRPCV1(ctx, "ws://127.0.0.1:123"+id+"/rpc/v1", headers)
	require.NoError(t, err)

	t.Cleanup(func() {
		closer()
	})

	return c
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

func waitForNodes(ctx context.Context, clients ...v1api.FullNode) error {
	g, ctx := errgroup.WithContext(ctx)

	timeout := time.After(300 * time.Second)

	for _, node := range clients {
		node := node
		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return fmt.Errorf("context cancelled")
				case <-timeout:
					return fmt.Errorf("time exceeded")
				default:
				}
				h, err := node.ChainHead(ctx)
				if err == nil || h.Height() >= 0 {
					return nil
				}
			}
		})
	}

	return g.Wait()
}
