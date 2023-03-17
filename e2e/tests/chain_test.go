package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-state-types/abi"
)

func TestSmoke_NetworkStarted(t *testing.T) {
	ctx := context.Background()

	nodes := ClientsFor(ctx, t, "0", "1", "2", "3")

	err := waitForNodes(ctx, nodes...)
	require.NoError(t, err)
}

func TestSmoke_ChainStarted(t *testing.T) {
	ctx := context.Background()

	c := ClientFor(ctx, t, "0")

	head, err := c.ChainHead(ctx)
	require.NoError(t, err)

	require.Greater(t, head.Height(), abi.ChainEpoch(1))
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

	err := waitForHeight(ctx, 10, nodes...)
	require.NoError(t, err)
}
