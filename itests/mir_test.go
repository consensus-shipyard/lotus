package itests

import (
	"context"
	"math/rand"
	"sync"
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

// TestMirConsensus tests that Mir operates normally.
//
// Notes:
// - It is assumed that the first F of N nodes can be byzantine
// - In terms of Go, that means that nodes[:MirFaultyValidatorNumber] can be byzantine,
//   and nodes[MirFaultyValidatorNumber:] are honest nodes.
func TestMirConsensus(t *testing.T) {
	require.Greater(t, MirFaultyValidatorNumber, 0)
	require.Equal(t, MirTotalValidatorNumber, MirHonestValidatorNumber+MirFaultyValidatorNumber)

	t.Run("mir", func(t *testing.T) {
		runTestDraft(t, kit.ThroughRPC())
		// runMirConsensusTests(t, kit.ThroughRPC())
	})
}

func runTestDraft(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("test", ts.testMirStartStop)
	// t.Run("testMirTwoNodesMining", ts.testMirTwoNodesMining)

}

func runMirConsensusTests(t *testing.T, opts ...interface{}) {
	ts := eudicoConsensusSuite{opts: opts}

	t.Run("testMirOneNodeMining", ts.testMirOneNodeMining)
	t.Run("testMirTwoNodesMining", ts.testMirTwoNodesMining)
	t.Run("testMirAllNodesMining", ts.testMirAllNodesMining)
	t.Run("testMirWhenLearnersJoin", ts.testMirWhenLearnersJoin)
	t.Run("testMirNodesStartWithRandomDelay", ts.testMirNodesStartWithRandomDelay)
	t.Run("testMirFNodesNeverStart", ts.testMirFNodesNeverStart)
	t.Run("testMirFNodesStartWithRandomDelay", ts.testMirFNodesStartWithRandomDelay)
	t.Run("testMirMiningWithMessaging", ts.testMirAllNodesMiningWithMessaging)
	t.Run("testMirWithFOmissionNodes", ts.testMirWithFOmissionNodes)
	t.Run("testMirWithFCrashedNodes", ts.testMirWithFCrashedNodes)
	t.Run("testMirWithFCrashedAndRecoveredNodes", ts.testMirWithFCrashedAndRecoveredNodes)
	t.Run("testMirFNodesCrashLongTimeApart", ts.testMirFNodesCrashLongTimeApart)
	t.Run("testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash", ts.testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash)
	t.Run("testMirFNodesSleepAndThenOperate", ts.testMirFNodesSleepAndThenOperate)
}

type eudicoConsensusSuite struct {
	opts []interface{}
}

// testMirOneNodeMining tests that a Mir node can mine blocks.
func (ts *eudicoConsensusSuite) testMirOneNodeMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	full, miner, ens := kit.EnsembleMinimalMir(t, ts.opts...)
	ens.BeginMirMining(ctx, &wg, miner)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, full)
	require.NoError(t, err)
}

// testMirTwoNodesMining tests that two Mir nodes can mine blocks.
//
// NOTE: Its peculiarity is that it uses other mechanisms to instantiate testing comparing to the main tests here.
func (ts *eudicoConsensusSuite) testMirTwoNodesMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
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

	// Fail if nodes have peers
	p, err := n1.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node one has peers")

	p, err = n2.NetPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, p, "node two has peers")

	ens.Connect(n1, n2).BeginMirMining(ctx, &wg, m1, m2)

	err = kit.AdvanceChain(ctx, TestedBlockNumber, n1, n2)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, n1, n2)
	require.NoError(t, err)
}

// testMirAllNodesMining tests that n nodes can mine blocks normally.
func (ts *eudicoConsensusSuite) testMirAllNodesMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

// testMirFNodesNeverStart tests that n − f nodes operate normally if f nodes never start.
func (ts *eudicoConsensusSuite) testMirFNodesNeverStart(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirHonestValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

// testMirWhenLearnersJoin tests that all nodes operate normally
// if new learner joins when the network is already started and syncs the whole network.
func (ts *eudicoConsensusSuite) testMirWhenLearnersJoin(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Log(">>> learners join")

	var learners []*kit.TestFullNode
	for i := 0; i < MirLearnersNumber; i++ {
		var learner kit.TestFullNode
		ens.FullNode(&learner, kit.LearnerNode()).Start().InterconnectFullNodes()
		require.Equal(t, true, learner.IsLearner())
		learners = append(learners, &learner)
	}

	err = kit.AdvanceChain(ctx, TestedBlockNumber, learners...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], append(nodes[1:], learners...)...)
	require.NoError(t, err)
}

// testMirNodesStartWithRandomDelay tests that all nodes eventually operate normally
// if all nodes start with large, random delays (1-2 minutes).
func (ts *eudicoConsensusSuite) testMirNodesStartWithRandomDelay(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMiningWithDelay(ctx, &wg, MaxDelay, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

// testMirNodesStartWithRandomDelay tests that all nodes eventually operate normally
// if f nodes start with large, random delays (1-2 minutes).
func (ts *eudicoConsensusSuite) testMirFNodesStartWithRandomDelay(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMiningWithDelayForFaultyNodes(ctx, &wg, MaxDelay, miners[MirFaultyValidatorNumber:], miners[:MirFaultyValidatorNumber]...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

// testMirAllNodesMiningWithMessaging tests that sending messages mechanism operates normally for all nodes.
func (ts *eudicoConsensusSuite) testMirAllNodesMiningWithMessaging(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

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

// testMirWithFOmissionNodes tests that n − f nodes operate normally and can recover
// if f nodes do not have access to network at the same time.
func (ts *eudicoConsensusSuite) testMirWithFOmissionNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> disconnecting %d Mir miners", MirFaultyValidatorNumber)
	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.DisconnectMirMiner(miners[i])
	}

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> reconnecting %d Mir miners", MirFaultyValidatorNumber)
	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.ReconnectMirMiner(miners[i])
	}

	err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes...)
	require.NoError(t, err)
}

// testMirWithFCrashedNodes tests that n − f nodes operate normally and can recover
// if f nodes crash at the same time.
func (ts *eudicoConsensusSuite) testMirWithFCrashedNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d miners", MirFaultyValidatorNumber)
	ens.CrashMirMiners(ctx, 0, miners[:MirFaultyValidatorNumber]...)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> restore %d miners", MirFaultyValidatorNumber)
	ens.RestoreMirMinersWithDB(ctx, miners[:MirFaultyValidatorNumber]...)

	// FIXME: Consider using advance chain instead of a time.Sleep here if possible.
	// err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	// require.NoError(t, err)
	time.Sleep(10 * time.Second)
	err = kit.CheckNodesInSync(ctx, 0, nodes[MirFaultyValidatorNumber], nodes...)
	require.NoError(t, err)
}

// testMirStartStop tests that n − f nodes operate normally and can recover
// if f nodes crash at the same time.
func (ts *eudicoConsensusSuite) testMirStartStop(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wait := make(chan struct{})
		go func() {
			wg.Wait()
			wait <- struct{}{}
		}()
		select {
		case <-time.After(10 * time.Second):
			t.Fatalf("fail to stop Mir nodes")
		case <-wait:
		}
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
}

// testMirWithFCrashedAndRecoveredNodes tests that n − f nodes operate normally without significant interruption,
// and recovered nodes eventually operate normally
// if f nodes crash and then recover (with only initial state) after a long delay (few minutes).
func (ts *eudicoConsensusSuite) testMirWithFCrashedAndRecoveredNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d miners", MirFaultyValidatorNumber)
	ens.CrashMirMiners(ctx, 0, miners[:MirFaultyValidatorNumber]...)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> restore %d miners from scratch", MirFaultyValidatorNumber)
	ens.RestoreMirMinersWithEmptyDB(ctx, miners[:MirFaultyValidatorNumber]...)

	err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes...)
	require.NoError(t, err)
}

// testMirFNodesCrashLongTimeApart tests that n − f nodes operate normally
// if f nodes crash, long time apart (few minutes).
func (ts *eudicoConsensusSuite) testMirFNodesCrashLongTimeApart(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d nodes", MirFaultyValidatorNumber)
	ens.CrashMirMiners(ctx, MaxDelay, miners[:MirFaultyValidatorNumber]...)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> restore %d nodes", MirFaultyValidatorNumber)
	ens.RestoreMirMinersWithDB(ctx, miners[:MirFaultyValidatorNumber]...)

	err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes...)
	require.NoError(t, err)
}

// testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash tests that n − f nodes operate normally
// and partitioned nodes eventually catch up
// if f nodes have a long period of no network access, but do not crash.
func (ts *eudicoConsensusSuite) testMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, ts.opts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> disconnecting %d Mir miners", MirFaultyValidatorNumber)
	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.DisconnectMirMiner(miners[i])
	}

	t.Logf(">>> delay")
	kit.RandomDelay(MaxDelay)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Log(">>> restoring network connections")
	for i := 0; i < MirFaultyValidatorNumber; i++ {
		ens.ReconnectMirMiner(miners[i])
	}

	err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes...)
	require.NoError(t, err)
}

// testMirFNodesSleepAndThenOperate tests that n − f nodes operate normally without significant interruption
// and woken up nodes eventually operate normally
// if f  nodes sleep for a significant amount of time and then continue operating but keep network connection.
func (ts *eudicoConsensusSuite) testMirFNodesSleepAndThenOperate(t *testing.T) {
	// TBD
}
