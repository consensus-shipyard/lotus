package itests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/itests/kit"
)

// TestTSPoWConsensus tests that PoW operates normally.
func TestTSPoWConsensus(t *testing.T) {
	t.Run("tspow", func(t *testing.T) {
		runTSPoWConsensusTests(t, kit.ThroughRPC(), kit.PoWConsensus())
	})
}

type itestsTSpoWConsensusSuite struct {
	opts []interface{}
}

func runTSPoWConsensusTests(t *testing.T, opts ...interface{}) {
	ts := itestsTSpoWConsensusSuite{opts: opts}

	t.Run("testTSPoWMining", ts.testTSPoWMining)
}

func (ts *itestsTSpoWConsensusSuite) testTSPoWMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, miner, ens := kit.EnsembleMinimalSpacenet(t, ts.opts...)
	ens.BeginTSPoWMining(ctx, &wg, miner)

	err := kit.AdvanceChain(ctx, 5, full)
	require.NoError(t, err)
}
