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

	// t.Run("testMirOneNodeMining", ts.testMirOneNodeMining)
	// t.Run("testMirTwoNodesMining", ts.testMirTwoNodesMining)
	// t.Run("testMirFourNodesMining", ts.testMirFourNodesMining)
	// t.Run("testMirFNodesNeverStart", ts.testMirFNodesNeverStart)
	// t.Run("testMirFNodesStartWithRandomDelay", ts.testMirFNodesStartWithRandomDelay)
	// t.Run("testMirSevenNodesMining", ts.testMirSevenNodesMining)
	// t.Run("testMirFourNodesMiningWithMessaging", ts.testMirFourNodesMiningWithMessaging)
	// t.Run("testMirWithOneOmissionNode", ts.testMirWithOmissionNodes)
	// t.Run("testMirWithCrashedNodes", ts.testMirWithCrashedNodes)
	t.Run("testMirFNodesCrashLongTimeApart", ts.testMirFNodesCrashLongTimeApart)
}

type eudicoConsensusSuite struct {
	opts []interface{}
}

func (ts *eudicoConsensusSuite) testMirOneNodeMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	full, miner, ens := kit.EnsembleMinimalMir(t, ts.opts...)
	ens.BeginMirMining(ctx, miner)

	err := kit.SubnetHeightCheck(ctx, 10, full)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirTwoNodesMining(t *testing.T) {
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

	err = kit.SubnetHeightCheck(ctx, 10, n1, n2)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirFourNodesMining(t *testing.T) {
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

	err := kit.SubnetHeightCheck(ctx, 10, nodes...)
	require.NoError(t, err)
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

	err := kit.SubnetHeightCheck(ctx, 10, nodes...)
	require.NoError(t, err)
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
	kit.Delay(120)

	ens.BeginMirMining(ctx, miners[0])

	t.Log(">>> Delay the second node")
	kit.Delay(120)

	ens.BeginMirMining(ctx, miners[1])

	err := kit.SubnetHeightCheck(ctx, 10, nodes...)
	require.NoError(t, err)
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

	err := kit.SubnetHeightCheck(ctx, 10, nodes...)
	require.NoError(t, err)
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

	addr1, err := nodes[1].WalletDefaultAddress(ctx)
	require.NoError(t, err)

	addr2, err := nodes[2].WalletDefaultAddress(ctx)
	require.NoError(t, err)

	msg := &types.Message{
		From:  addr1,
		To:    addr2,
		Value: big.Zero(),
	}

	smsg, err := nodes[1].MpoolPushMessage(ctx, msg, nil)
	require.NoError(t, err)

	err = kit.MirNodesWaitMsg(ctx, smsg.Cid(), nodes...)
	require.NoError(t, err)

	addr3, err := nodes[3].WalletDefaultAddress(ctx)
	require.NoError(t, err)

	msg = &types.Message{
		From:  addr2,
		To:    addr3,
		Value: big.Zero(),
	}

	smsg, err = nodes[2].MpoolPushMessage(ctx, msg, nil)
	require.NoError(t, err)

	err = kit.MirNodesWaitMsg(ctx, smsg.Cid(), nodes...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirWithOmissionNodes(t *testing.T) {
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

	err := kit.SubnetHeightCheck(ctx, 10, nodes...)

	ens.Disconnect(nodes[0], nodes[1], nodes[2], nodes[3], nodes[4], nodes[5], nodes[6])
	ens.Disconnect(nodes[1], nodes[2], nodes[3], nodes[4], nodes[5], nodes[6])

	err = kit.SubnetHeightCheck(ctx, 10, nodes[2:]...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirWithCrashedNodes(t *testing.T) {
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

	err := kit.SubnetHeightCheck(ctx, 10, append(nodes, crashedNode)...)
	require.NoError(t, err)

	crashNode()

	err = kit.SubnetHeightCheck(ctx, 10, nodes...)
	require.NoError(t, err)
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

	err := kit.SubnetHeightCheck(ctx, 10, append(nodes, crashedNode1, crashedNode2)...)
	require.NoError(t, err)

	crashNode1()

	kit.Delay(120)
	crashNode2()

	err = kit.SubnetHeightCheck(ctx, 10, nodes...)
	require.NoError(t, err)
}
