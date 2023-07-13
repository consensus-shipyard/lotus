// Package tests contains end-to-end tests for Eudico.
package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/e2e/internal/fs"
)

var (
	WaitTimeout = 5 * time.Minute

	NetworkSize     int
	DeploymentPath  string
	MirConfigPath   string
	ManifestDirPath string
)

func init() {
	r, err := fs.FindRoot()
	if err != nil {
		panic(err)
	}
	DeploymentPath, err = filepath.Abs(filepath.Join(r, "e2e", "testdata", "_runtime"))
	if err != nil {
		panic(err)
	}
	MirConfigPath, err = filepath.Abs(filepath.Join(r, "e2e", "testdata", "mir"))
	if err != nil {
		panic(err)
	}
	ManifestDirPath, err = filepath.Abs(filepath.Join(r, "e2e", "networks"))
	if err != nil {
		panic(err)
	}

}

func getAuthToken(id string) (string, error) {
	b, err := os.ReadFile(path.Join(DeploymentPath, id, "token"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func waitForAuthToken(id string) error {
	timeout := time.After(WaitTimeout)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("[%s] waitForAuthToken:time exceeded", id)
		default:
		}

		if _, err := os.Stat(path.Join(DeploymentPath, id, "token")); errors.Is(err, os.ErrNotExist) {
			time.Sleep(1 * time.Second)
			fmt.Printf("[%s] waitForAuthToken: wait for node %s Lotus token...\n", id, id)
			continue
		}
		fmt.Printf("[%s] waitForAuthToken: Lotus token is ready\n", id)
		return nil
	}
}

func waitForLotusAPI(id string) error {
	timeout := time.After(WaitTimeout)

	ctx := context.Background()

	token, err := getAuthToken(id)
	if err != nil {
		return err
	}

	headers := http.Header{"Authorization": []string{"Bearer " + token}}

	c, closer, err := client.NewFullNodeRPCV1(ctx, "ws://127.0.0.1:123"+id+"/rpc/v1", headers)
	if err != nil {
		return err
	}
	defer closer()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("[%s] waitForLotusAPI: time exceeded", id)
		default:
		}

		if _, err := c.Version(ctx); errors.Is(err, os.ErrNotExist) {
			time.Sleep(1 * time.Second)
			fmt.Printf("[%s] waitForLotusAPI: wait for node %s Lotus API...\n", id, id)
			continue
		}
		fmt.Printf("[%s] waitForLotusAPI: LotusAPI is accessible\n", id)
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

	headers := http.Header{"Authorization": []string{"Bearer " + token}}

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
