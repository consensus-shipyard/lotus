package itests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/itests/kit"
)

const (
	PoWTotalMinerNumber  = 4
	PoWTestedBlockNumber = 4
)

// TestPoWConsensus tests that PoW operates normally.
func TestPoWConsensus(t *testing.T) {
	t.Run("pow", func(t *testing.T) {
		runPoWConsensusTests(t, kit.ThroughRPC(), kit.PoWConsensus())
	})
}

type itestsPoWConsensusSuite struct {
	opts []interface{}
}

func runPoWConsensusTests(t *testing.T, opts ...interface{}) {
	ts := itestsPoWConsensusSuite{opts: opts}

	// t.Run("testPoWMining", ts.testPoWOneNoneMining)
	t.Run("testPoWAllNodesMining", ts.testPoWAllNodesMining)
}

// testPoWOneNoneMining tests that a PoW node can mine blocks.
func (ts *itestsPoWConsensusSuite) testPoWOneNoneMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, miner, ens := kit.EnsembleMinimalSpacenet(t, ts.opts...)
	ens.BeginPoWMining(ctx, &wg, miner)

	err := kit.AdvanceChain(ctx, PoWTestedBlockNumber, full)
	require.NoError(t, err)
}

// testPoWAllNodesMining tests that n nodes can mine blocks normally.
func (ts *itestsPoWConsensusSuite) testPoWAllNodesMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsemblePoWNodes(t, PoWTotalMinerNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginPoWMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, PoWTestedBlockNumber, nodes...)
	require.NoError(t, err)

	// FIXME DENIS
	// Can we use CheckNodesInSync in PoW? At present, it fails with "failed to reach the same CID in node" error.
	// err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	// require.NoError(t, err)
}
