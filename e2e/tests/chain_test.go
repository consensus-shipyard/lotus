package tests

import (
	"context"
	"testing"

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
