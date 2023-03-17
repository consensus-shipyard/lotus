package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestMain(m *testing.M) {
	err := waitForAuthToken("0")
	if err != nil {
		panic(err)
	}
	err = waitForLotusAPI("0")
	if err != nil {
		panic(err)
	}
	m.Run()
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
