// Package tests contains end-to-end tests for Eudico nodes.
package tests

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
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

const DeploymentPath = "./testdata/_runtime"

func getAuthToken(id string) (string, error) {
	b, err := ioutil.ReadFile(path.Join(DeploymentPath, id, "/token"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func waitForAuthToken(id string) error {
	timeout := time.After(120 * time.Second)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("time exceeded")
		default:
		}

		if _, err := os.Stat(path.Join(DeploymentPath, id, "/token")); errors.Is(err, os.ErrNotExist) {
			time.Sleep(1 * time.Second)
			fmt.Println("wait for Lotus Token...")
			continue
		}
		return nil
	}
}

func waitForLotusAPI(id string) error {
	timeout := time.After(120 * time.Second)

	ctx := context.Background()

	token, err := getAuthToken(id)
	if err != nil {
		return err
	}

	headers := http.Header{"Authorization": []string{"Bearer " + string(token)}}

	c, closer, err := client.NewFullNodeRPCV1(ctx, "ws://127.0.0.1:123"+id+"/rpc/v1", headers)
	if err != nil {
		return err
	}
	defer closer()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("time exceeded")
		default:
		}

		if _, err := c.Version(ctx); errors.Is(err, os.ErrNotExist) {
			time.Sleep(1 * time.Second)
			fmt.Printf("wait for node %s Lotus API...\n", id)
			continue
		}

		return nil
	}
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
