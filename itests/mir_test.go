package itests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/itests/kit"
	mapi "github.com/filecoin-project/mir"
)

func TestMirConsensus(t *testing.T) {
	t.Run("mir", func(t *testing.T) {
		runMirConsensusTests(t, kit.ThroughRPC())
	})
}

func runMirConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testMirMiningOneNode", ts.testMirMiningOneNode)
	t.Run("testMirMiningTwoNodes", ts.testMirMiningTwoNodes)
	t.Run("testMirMiningFourNodes", ts.testMirMiningFourNodes)
}

type eudicoConsensusSuite struct {
	opts []interface{}
}

func (ts *eudicoConsensusSuite) testMirMiningOneNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, miner, ens := kit.EnsembleMinimalMir(t, ts.opts...)
	ens.BeginMirMining(ctx, miner)

	err := kit.SubnetHeightCheckForBlocks(ctx, 10, full)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirMiningTwoNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	n1, n2, m1, m2, ens := kit.EnsembleTwoMirNodes(t, ts.opts...)

	// Fail if genesis blocks are different
	gen1, err := n1.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := n2.ChainGetGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gen1.String(), gen2.String())

	// Fail if no peers
	p, err := n1.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node one has peers")

	p, err = n2.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node two has peers")

	ens.Connect(n1, n2)

	ens.BeginMirMining(ctx, m1, m2)

	err = kit.SubnetHeightCheckForBlocks(ctx, 10, n1)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}
	require.NoError(t, err)

	err = kit.SubnetHeightCheckForBlocks(ctx, 10, n2)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}
	require.NoError(t, err)

}

func (ts *eudicoConsensusSuite) testMirMiningFourNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	n1, n2, n3, n4, m1, m2, m3, m4, ens := kit.EnsembleFourMirNodes(t, ts.opts...)

	// Fail if genesis blocks are different
	gen1, err := n1.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := n2.ChainGetGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gen1.String(), gen2.String())

	// Fail if no peers
	p, err := n1.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node one has peers")

	p, err = n2.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node two has peers")

	p, err = n3.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node three has peers")

	p, err = n4.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node four has peers")

	ens.Connect(n1, n2, n3, n4)

	ens.BeginMirMining(ctx, m1, m2, m3, m4)

	err = kit.SubnetHeightCheckForBlocks(ctx, 5, n1)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}
	require.NoError(t, err)

	err = kit.SubnetHeightCheckForBlocks(ctx, 5, n2)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}

	err = kit.SubnetHeightCheckForBlocks(ctx, 5, n3)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}

	err = kit.SubnetHeightCheckForBlocks(ctx, 5, n4)
	if xerrors.Is(mapi.ErrStopped, err) {
		return
	}
	require.NoError(t, err)

}
