package kit

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/types"
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

func adaptForMir(t *testing.T, full *TestFullNode, miner *TestMiner) {
	addr, err := full.WalletNew(context.Background(), types.KTSecp256k1)
	require.NoError(t, err)

	pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
	require.NoError(t, err)

	h, err := libp2p.New(
		libp2p.Identity(pk),
		libp2p.DefaultTransports,
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	require.NoError(t, err)

	miner.mirHost = h
	miner.mirAddr = addr
}

// EnsembleMinimalMir creates and starts an Ensemble suitable for Mir.
func EnsembleMinimalMir(t *testing.T, opts ...interface{}) (*TestFullNode, *TestMiner, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		full  TestFullNode
		miner TestMiner
	)
	ens := NewEnsemble(t, eopts...).FullNode(&full, nopts...).Miner(&miner, &full, nopts...)

	ens.active.miners = []*TestMiner{}
	ens.Start()

	adaptForMir(t, &full, &miner)

	return &full, &miner, ens
}

func EnsembleTwoMirNodes(t *testing.T, opts ...interface{}) (
	*TestFullNode, *TestFullNode,
	*TestMiner, *TestMiner,
	*Ensemble,
) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		n1, n2 TestFullNode
		m1, m2 TestMiner
	)
	ens := NewEnsemble(t, eopts...).FullNode(&n1, nopts...).FullNode(&n2, nopts...).Miner(&m1, &n1, nopts...).Miner(&m2, &n2, nopts...)
	ens.active.miners = []*TestMiner{}
	ens.Start()

	adaptForMir(t, &n1, &m1)
	adaptForMir(t, &n2, &m2)

	return &n1, &n2, &m1, &m2, ens
}

func EnsembleFourMirNodes(t *testing.T, opts ...interface{}) (
	*TestFullNode, *TestFullNode, *TestFullNode, *TestFullNode,
	*TestMiner, *TestMiner, *TestMiner, *TestMiner,
	*Ensemble,
) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		n1, n2, n3, n4 TestFullNode
		m1, m2, m3, m4 TestMiner
	)
	ens := NewEnsemble(t, eopts...).
		FullNode(&n1, nopts...).
		FullNode(&n2, nopts...).
		FullNode(&n3, nopts...).
		FullNode(&n4, nopts...).
		Miner(&m1, &n1, nopts...).
		Miner(&m2, &n2, nopts...).
		Miner(&m3, &n3, nopts...).
		Miner(&m4, &n4, nopts...)

	ens.active.miners = []*TestMiner{}
	ens.Start()

	adaptForMir(t, &n1, &m1)
	adaptForMir(t, &n2, &m2)
	adaptForMir(t, &n3, &m3)
	adaptForMir(t, &n4, &m4)

	return &n1, &n2, &n3, &n4, &m1, &m2, &m3, &m4, ens
}

func EnsembleMirNodes(t *testing.T, size int, opts ...interface{}) ([]*TestFullNode, []*TestMiner, *Ensemble) {
	opts = append(opts, WithAllSubsystems())

	eopts, nopts := siftOptions(t, opts)

	var (
		nodes  []*TestFullNode
		miners []*TestMiner
	)

	ens := NewEnsemble(t, eopts...)

	for i := 0; i < size; i++ {
		var n TestFullNode
		var m TestMiner
		ens.FullNode(&n, nopts...).Miner(&m, &n, nopts...)
		nodes = append(nodes, &n)
		miners = append(miners, &m)
	}

	ens.active.miners = []*TestMiner{}
	ens.Start()

	for i := 0; i < size; i++ {
		adaptForMir(t, nodes[i], miners[i])
	}

	return nodes, miners, ens
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
