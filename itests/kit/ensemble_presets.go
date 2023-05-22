package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// EnsembleMinimal creates and starts an Ensemble with a single full node and a single miner.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EnsembleMinimal(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full  TestFullNode
		miner TestMiner
	)
	ens := NewEnsemble(t, eopts...).FullNode(&full, nopts...).Miner(&miner, &full, nopts...).Start()
	return &full, &miner, ens
}

func EnsembleWithMirValidators(t *testing.T, n int, opts ...interface{}) ([]*TestFullNode, []*TestValidator, *Ensemble) {
	opts = append(opts, WithAllSubsystems(), ThroughRPC(), MirConsensus())

	eopts, nopts := siftOptions(t, opts)

	var (
		nodes      []*TestFullNode
		miners     []*TestMiner
		validators []*TestValidator
	)

	ens := NewEnsemble(t, eopts...)

	for i := 0; i < n; i++ {
		var node TestFullNode
		var miner TestMiner
		ens.FullNode(&node, nopts...).Miner(&miner, &node, nopts...)
		nodes = append(nodes, &node)
		miners = append(miners, &miner)
	}

	ens.active.miners = []*TestMiner{}
	ens.Start()

	for i := 0; i < n; i++ {
		validators = append(validators, NewTestValidator(t, nodes[i], *miners[i]))
	}

	require.Equal(t, n, len(nodes))
	require.Equal(t, n, len(miners))

	return nodes, validators, ens
}

func AreTwins(t *testing.T, miners []*TestValidator, twins []*TestValidator) {
	for _, v := range miners {
		fmt.Println(v.mirAddr)
	}
	for _, v := range twins {
		fmt.Println(v.mirAddr)
	}

	for i, miner := range miners {
		require.Equal(t, miner.mirAddr, twins[i].mirAddr)
		require.Equal(t, miner.mirPrivKey, twins[i].mirPrivKey)
	}
}

func EnsembleMirNodesWithByzantineTwins(t *testing.T, n int, opts ...interface{}) ([]*TestFullNode, []*TestFullNode, []*TestValidator, []*TestValidator, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		nodes      []*TestFullNode
		miners     []*TestMiner
		validators []*TestValidator

		twinNodes      []*TestFullNode
		twinMiners     []*TestMiner
		twinValidators []*TestValidator
	)

	ens := NewEnsemble(t, eopts...)

	f := (n - 1) / 3

	for i := 0; i < n; i++ {
		var node TestFullNode
		var miner TestMiner
		ens.FullNode(&node, nopts...).Miner(&miner, &node, nopts...)
		nodes = append(nodes, &node)
		miners = append(miners, &miner)
	}

	ens.active.miners = []*TestMiner{}
	ens.Start()

	for i := 0; i < n; i++ {
		validators = append(validators, NewTestValidator(t, nodes[i], *miners[i]))
	}

	for i := 0; i < f; i++ {
		twinNodes = append(twinNodes, nodes[n+i-1])
		twinMiners = append(twinMiners, miners[n+i-1])
		twinValidators = append(twinValidators, validators[n+i-1])
	}

	require.Equal(t, n, len(nodes))
	require.Equal(t, n, len(miners))

	require.Equal(t, f, len(twinNodes))
	require.Equal(t, f, len(twinMiners))

	require.Equal(t, len(miners[n-f:]), len(twinMiners))
	require.Equal(t, len(nodes[n-f:]), len(twinNodes))

	AreTwins(t, validators[n-f:], twinValidators)

	return nodes, twinNodes, validators, twinValidators, ens
}

func EnsembleMirNodesWithLearner(t *testing.T, n int, opts ...interface{}) ([]*TestFullNode, []*TestValidator, *TestFullNode, *Ensemble) {
	var learner TestFullNode
	nodes, validators, ens := EnsembleWithMirValidators(t, n, opts)
	ens.FullNode(&learner, LearnerNode()).Start()

	return nodes, validators, &learner, ens
}

func EnsembleWorker(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *TestWorker, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full   TestFullNode
		miner  TestMiner
		worker TestWorker
	)
	ens := NewEnsemble(t, eopts...).FullNode(&full, nopts...).Miner(&miner, &full, nopts...).Worker(&miner, &worker, nopts...).Start()
	return &full, &miner, &worker, ens
}

func EnsembleWithMinerAndMarketNodes(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *TestMiner, *Ensemble) {
	eopts, nopts := siftOptions(t, opts)

	var (
		fullnode     TestFullNode
		main, market TestMiner
	)

	mainNodeOpts := []NodeOpt{WithSubsystems(SSealing, SSectorStorage, SMining), DisableLibp2p()}
	mainNodeOpts = append(mainNodeOpts, nopts...)

	blockTime := 100 * time.Millisecond
	ens := NewEnsemble(t, eopts...).FullNode(&fullnode, nopts...).Miner(&main, &fullnode, mainNodeOpts...).Start()
	ens.BeginMining(blockTime)

	marketNodeOpts := []NodeOpt{OwnerAddr(fullnode.DefaultKey), MainMiner(&main), WithSubsystems(SMarkets)}
	marketNodeOpts = append(marketNodeOpts, nopts...)

	ens.Miner(&market, &fullnode, marketNodeOpts...).Start().Connect(market, fullnode)

	return &fullnode, &main, &market, ens
}

// EnsembleTwoOne creates and starts an Ensemble with two full nodes and one miner.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EnsembleTwoOne(t *testing.T, opts ...interface{}) (*TestFullNode, *TestFullNode, *TestMiner, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		one, two TestFullNode
		miner    TestMiner
	)
	ens := NewEnsemble(t, eopts...).FullNode(&one, nopts...).FullNode(&two, nopts...).Miner(&miner, &one, nopts...).Start()
	return &one, &two, &miner, ens
}

// EnsembleOneTwo creates and starts an Ensemble with one full node and two miners.
// It does not interconnect nodes nor does it begin mining.
//
// This function supports passing both ensemble and node functional options.
// Functional options are applied to all nodes.
func EnsembleOneTwo(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *TestMiner, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full     TestFullNode
		one, two TestMiner
	)
	ens := NewEnsemble(t, eopts...).
		FullNode(&full, nopts...).
		Miner(&one, &full, nopts...).
		Miner(&two, &full, nopts...).
		Start()

	return &full, &one, &two, ens
}

func siftOptions(t *testing.T, opts []interface{}) (eopts []EnsembleOpt, nopts []NodeOpt) {
	for _, v := range opts {
		switch o := v.(type) {
		case EnsembleOpt:
			eopts = append(eopts, o)
		case NodeOpt:
			nopts = append(nopts, o)
		default:
			t.Fatalf("invalid option type: %T", o)
		}
	}
	return eopts, nopts
}
