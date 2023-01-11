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

// TestPoWOneNodeMining tests that PoW operates normally on one node.
func TestPoWOneNodeMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Log("[*] defer: cancelling test context")
		cancel()
		wg.Wait()
	}()

	full, miner, ens := kit.EnsembleMinimalSpacenet(t, kit.ThroughRPC(), kit.PoWConsensus())
	ens.BeginPoWMining(ctx, &wg, miner)

	err := kit.AdvanceChain(ctx, PoWTestedBlockNumber, full)
	require.NoError(t, err)
}

// TestPoWAllNodesMining tests that PoW operates normally on N nodes.
func TestPoWAllNodesMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsemblePoWNodes(t, PoWTotalMinerNumber, kit.ThroughRPC(), kit.PoWConsensus())
	ens.InterconnectFullNodes().BeginPoWMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, PoWTestedBlockNumber, nodes...)
	require.NoError(t, err)

	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}
