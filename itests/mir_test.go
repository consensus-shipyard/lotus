package itests

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

const (
	MirTotalValidatorNumber  = 4 // N = 3F+1
	MirFaultyValidatorNumber = (MirTotalValidatorNumber - 1) / 3
	MirHonestValidatorNumber = MirTotalValidatorNumber - MirFaultyValidatorNumber
	MirLearnersNumber        = MirFaultyValidatorNumber + 1
	TestedBlockNumber        = 10
	MaxDelay                 = 30
)

func TestMirConsensus(t *testing.T) {
	require.Greater(t, MirFaultyValidatorNumber, 0)
	require.Equal(t, MirTotalValidatorNumber, MirHonestValidatorNumber+MirFaultyValidatorNumber)

	t.Run("mir", func(t *testing.T) {
		// runTestDraft(t, kit.ThroughRPC())
		runMirConsensusTests(t, kit.ThroughRPC())
	})
}

func runTestDraft(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testMirTwoNodesMining", ts.testMirWithFCrashedNodes)
	// t.Run("testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash", ts.testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash)
}

func runMirConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testMirOneNodeMining", ts.testMirOneNodeMining)
	t.Run("testMirTwoNodesMining", ts.testMirTwoNodesMining)
	t.Run("testMirAllNodesMining", ts.testMirAllNodesMining)
	t.Run("testMirWhenLearnersJoin", ts.testMirWhenLearnersJoin)
	// t.Run("testMirFNodesNeverStart", ts.testMirFNodesNeverStart)
	// t.Run("testMirFNodesStartWithRandomDelay", ts.testMirFNodesStartWithRandomDelay)
	// t.Run("testMirMiningWithMessaging", ts.testMirAllNodesMiningWithMessaging)
	// t.Run("testMirWithFOmissionNodes", ts.testMirWithFOmissionNodes)
	// t.Run("testMirWithFCrashedNodes", ts.testMirWithFCrashedNodes)
	// t.Run("testMirFNodesCrashLongTimeApart", ts.testMirFNodesCrashLongTimeApart)
	// t.Run("testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash", ts.testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash)
}

type eudicoConsensusSuite struct {
	opts []interface{}
}

// testMirOneNodeMining tests that a Mir node can mine blocks.
func (ts *eudicoConsensusSuite) testMirOneNodeMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	full, miner, ens := kit.EnsembleMinimalMir(t, ts.opts...)
	ens.BeginMirMining(ctx, miner)

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, full)
	require.NoError(t, err)
}

// testMirTwoNodesMining tests that two Mir nodes can mine blocks.
//
// Its peculiarity is that it uses other mechanisms to instantiate testing comparing to the main tests here.
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

	ens.Connect(n1, n2).BeginMirMining(ctx, m1, m2)

	err = kit.ChainHeightCheck(ctx, TestedBlockNumber, n1, n2)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, n1, n2)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirAllNodesMining(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)

	for i, n := range nodes {
		p, err := n.NetPeers(ctx)
		require.NoError(t, err)
		require.Empty(t, p, "node has peers", "nodeID", i)
	}

	ens.InterconnectFullNodes().BeginMirMining(ctx, miners...)

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirFNodesNeverStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirHonestValidatorNumber, ts.opts...)

	ens.InterconnectFullNodes().BeginMirMining(ctx, miners...)

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirWhenLearnersJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, miners...)

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Log(">>> learners join")

	var learners []*kit.TestFullNode
	for i := 0; i < MirLearnersNumber; i++ {
		var learner kit.TestFullNode
		ens.FullNode(&learner, kit.LearnerNode()).Start().InterconnectFullNodes()
		require.Equal(t, true, learner.IsLearner())
		learners = append(learners, &learner)
	}

	err = kit.CheckNodesInSync(ctx, 0, nodes[0], learners...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirFNodesStartWithRandomDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes()
	ens.BeginMirMining(ctx, miners[MirFaultyValidatorNumber:]...)

	for i := 0; i < MirFaultyValidatorNumber; i++ {
		t.Logf(">>> Delay %d faulty node", i)
		kit.RandomDelay(MaxDelay)
		ens.BeginMirMining(ctx, miners[i])
	}

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirAllNodesMiningWithMessaging(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, miners...)

	for range nodes {
		rand.Seed(time.Now().UnixNano())
		j := rand.Intn(len(nodes))
		src, err := nodes[j].WalletDefaultAddress(ctx)
		require.NoError(t, err)

		dst, err := nodes[(j+1)%len(nodes)].WalletDefaultAddress(ctx)
		require.NoError(t, err)

		t.Logf(">>> node %s is sending a message to node %s", src, dst)

		smsg, err := nodes[j].MpoolPushMessage(ctx, &types.Message{
			From:  src,
			To:    dst,
			Value: big.Zero(),
		}, nil)
		require.NoError(t, err)

		err = kit.MirNodesWaitMsg(ctx, smsg.Cid(), nodes...)
		require.NoError(t, err)
	}
}

func (ts *eudicoConsensusSuite) testMirWithFOmissionNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, miners...)

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> disconnecting %d omission nodes", MirFaultyValidatorNumber)
	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.DisconnectNodes(nodes[i], nodes[i+1:]...)
	}

	err = kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Log(">>> restoring network connections")
	ens.InterconnectFullNodes()

	baseNode := nodes[MirFaultyValidatorNumber]
	err = kit.CheckNodesInSync(ctx, 0, baseNode, nodes[MirFaultyValidatorNumber:]...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirWithFCrashedNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes()
	crashNodes := ens.BeginMirMiningWithCrashes(ctx, miners...)
	require.Equal(t, MirTotalValidatorNumber, len(crashNodes))

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d nodes", MirFaultyValidatorNumber)

	for i := 0; i < MirFaultyValidatorNumber; i++ {
		crashNodes[i]()
	}

	t.Logf(">>> restore %d nodes", MirFaultyValidatorNumber)

	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.RestoreMirMining(ctx, i, miners...)
	}

	baseNode := nodes[MirFaultyValidatorNumber]
	err = kit.CheckNodesInSync(ctx, 0, baseNode, nodes[MirFaultyValidatorNumber:]...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirFNodesCrashLongTimeApart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes()
	crasheNode := ens.BeginMirMiningWithCrashes(ctx, miners...)

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d nodes", MirFaultyValidatorNumber)

	for i := 0; i < MirFaultyValidatorNumber; i++ {
		crasheNode[i]()
		kit.RandomDelay(MaxDelay)
	}

	t.Logf(">>> restore %d nodes", MirFaultyValidatorNumber)

	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.RestoreMirMining(ctx, i, miners...)
	}

	baseNode := nodes[MirFaultyValidatorNumber]
	err = kit.CheckNodesInSync(ctx, 0, baseNode, nodes[MirFaultyValidatorNumber:]...)
	require.NoError(t, err)
}

func (ts *eudicoConsensusSuite) testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, miners...)

	err := kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> disconnecting %d nodes", MirFaultyValidatorNumber)
	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.DisconnectNodes(nodes[i], nodes[i+1:]...)
	}

	err = kit.ChainHeightCheck(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:]...)
	require.NoError(t, err)

	kit.RandomDelay(MaxDelay)

	t.Log(">>> restoring network connections")
	ens.InterconnectFullNodes()

	baseNode := nodes[MirFaultyValidatorNumber]
	err = kit.CheckNodesInSync(ctx, 0, baseNode, nodes[MirFaultyValidatorNumber:]...)
	require.NoError(t, err)

}
