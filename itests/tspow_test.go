package itests

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/itests/kit"
)

// TestMirConsensus tests that Mir operates normally.
//
// Notes:
//   - It is assumed that the first F of N nodes can be byzantine
//   - In terms of Go, that means that nodes[:MirFaultyValidatorNumber] can be byzantine,
//     and nodes[MirFaultyValidatorNumber:] are honest nodes.
func TestTSPoWConsensus(t *testing.T) {
	t.Run("tspow", func(t *testing.T) {
		runTSPoWConsensusTests(t, kit.ThroughRPC())
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

	full, miner, ens := kit.EnsembleMinimalMir(t, ts.opts...)
	ens.BeginTSPoWMining(ctx, &wg, miner)

	err := kit.AdvanceChain(ctx, 3, full)
	require.NoError(t, err)
}
