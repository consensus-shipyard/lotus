package itests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestMirConsensus(t *testing.T) {
	t.Run("mir", func(t *testing.T) {
		runMirConsensusTests(t, kit.ThroughRPC())
	})
}

func runMirConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	// t.Run("testMirOneNodeMining", ts.testmirOnenodemining)
	// t.Run("testMirTwoNodesMining", ts.testmirTwonodesmining)
	// t.Run("testMirFourNodesMining", ts.testmirFournodesmining)
	// t.Run("testMirFNodesNeverStart", ts.testMirFNodesNeverStart)
	// t.Run("testMirFNodesStartWithRandomDelay", ts.testMirFNodesStartWithRandomDelay)
	t.Run("testMirSevenNodesMining", ts.testMirSevenNodesMining)
	// t.Run("testMirFourNodesMiningWithMessaging", ts.testMirFourNodesMiningWithMessaging)
	// t.Run("testMirFourNodesWithOneOmissionNode", ts.testMirFourNodesWithOneOmissionNode)
	// t.Run("testMirFourNodesWithOneCrashedNode", ts.testMirFourNodesWithOneCrashedNode)
	// t.Run("testMir_FNodesCrashLongTimeApart", ts.testMirFNodesCrashLongTimeApart)
}

type eudicoConsensusSuite struct {
	opts []interface{}
}

func (ts *eudicoConsensusSuite) testmirOnenodemining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	full, miner, ens := kit.EnsembleMinimalMir(t, ts.opts...)
	ens.BeginMirMining(ctx, miner)

	err := kit.SubnetHeightCheckForBlocks(ctx, 10, full)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testmirTwonodesmining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	n1, n2, m1, m2, ens := kit.EnsembleTwoMirNodes(t, ts.opts...)

	// Fail if genesis blocks are different
	gen1, err := n1.ChainGetGenesis(ctx)
	require.NoError(t, err)
	gen2, err := n2.ChainGetGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, gen1.String(), gen2.String())

	// Fail if nodes have peers
	p, err := n1.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node one has peers")

	p, err = n2.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node two has peers")

	ens.Connect(n1, n2)

	ens.BeginMirMining(ctx, m1, m2)

	err = kit.SubnetHeightCheckForBlocks(ctx, 10, n1)
	require.NoError(t, err)

	err = kit.SubnetHeightCheckForBlocks(ctx, 10, n2)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testmirFournodesmining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, 4, ts.opts...)

	for i, n := range nodes {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.InterconnectFullNodes()

	ens.BeginMirMining(ctx, miners...)

	for _, n := range nodes {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}
}

func (ts *eudicoConsensusSuite) testMirFNodesNeverStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, 3, ts.opts...)

	for i, n := range nodes {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.InterconnectFullNodes()

	ens.BeginMirMining(ctx, miners...)

	for _, n := range nodes {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}
}

func (ts *eudicoConsensusSuite) testMirFNodesStartWithRandomDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, 7, ts.opts...)

	for i, n := range nodes {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.InterconnectFullNodes()

	ens.BeginMirMining(ctx, miners[2:]...)

	t.Log(">>> Delay the first node")
	kit.Delay(200)

	ens.BeginMirMining(ctx, miners[0])

	t.Log(">>> Delay the second node")
	kit.Delay(200)

	ens.BeginMirMining(ctx, miners[1])

	for _, n := range nodes {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}
}

func (ts *eudicoConsensusSuite) testMirSevenNodesMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, 7, ts.opts...)

	for i, n := range nodes {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.InterconnectFullNodes()

	ens.BeginMirMining(ctx, miners...)

	for _, n := range nodes {
		err := kit.SubnetHeightCheckForBlocks(ctx, 30, n)
		require.NoError(t, err)
	}
}

func (ts *eudicoConsensusSuite) testMirFourNodesMiningWithMessaging(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, 4, ts.opts...)

	for i, n := range nodes {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.InterconnectFullNodes()

	ens.BeginMirMining(ctx, miners...)

	addrs1, err := nodes[1].WalletDefaultAddress(ctx)
	require.NoError(t, err)

	addrs2, err := nodes[2].WalletDefaultAddress(ctx)
	require.NoError(t, err)

	msg1 := &types.Message{
		From:  addrs1,
		To:    addrs2,
		Value: big.Zero(),
	}

	smsg1, err := nodes[1].MpoolPushMessage(ctx, msg1, nil)
	require.NoError(t, err)

	nodes[2].WaitMsg(ctx, smsg1.Cid())

	err = kit.MirNodesWaitMsg(ctx, smsg1.Cid(), nodes...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirFourNodesWithOneOmissionNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, 4, ts.opts...)

	for i, n := range nodes {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.InterconnectFullNodes()

	ens.BeginMirMining(ctx, miners...)

	for _, n := range nodes {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}

	ens.Disconnect(nodes[0], nodes[1], nodes[2], nodes[3])

	for _, n := range nodes[1:] {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}

}

func (ts *eudicoConsensusSuite) testMirFourNodesWithOneCrashedNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	crashedCtx, crashNode := context.WithCancel(ctx)

	crashedNode, crashedMiner, crashedEns := kit.EnsembleMinimalMir(t, ts.opts...)
	nodes, miners, ens := kit.EnsembleMirNodes(t, 3, ts.opts...)

	for i, n := range append(nodes, crashedNode) {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.Connect(crashedNode, nodes[0], nodes[1], nodes[2])

	crashedEns.BeginMirMining(crashedCtx, crashedMiner)

	ens.BeginMirMining(ctx, miners...)

	for _, n := range append(nodes, crashedNode) {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}

	crashNode()

	for _, n := range nodes {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}

}

func (ts *eudicoConsensusSuite) testMirFNodesCrashLongTimeApart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	crashedCtx1, crashNode1 := context.WithCancel(ctx)
	crashedCtx2, crashNode2 := context.WithCancel(ctx)

	crashedNode1, crashedMiner1, crashedEns1 := kit.EnsembleMinimalMir(t, ts.opts...)
	crashedNode2, crashedMiner2, crashedEns2 := kit.EnsembleMinimalMir(t, ts.opts...)
	nodes, miners, ens := kit.EnsembleMirNodes(t, 5, ts.opts...)

	for i, n := range append(nodes, crashedNode1, crashedNode2) {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.Connect(crashedNode1, crashedNode2, nodes[0], nodes[1], nodes[2], nodes[3], nodes[4])

	crashedEns1.BeginMirMining(crashedCtx1, crashedMiner1)
	crashedEns2.BeginMirMining(crashedCtx2, crashedMiner2)
	ens.BeginMirMining(ctx, miners...)

	for _, n := range append(nodes, crashedNode1, crashedNode2) {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}

	crashNode1()

	kit.Delay(120)
	crashNode2()

	for _, n := range nodes {
		err := kit.SubnetHeightCheckForBlocks(ctx, 10, n)
		require.NoError(t, err)
	}

}
