package itests

// These tests check that Eudico/Mir bundle operates normally.
//
// Notes:
//   - It is assumed that the first F of N nodes can be byzantine;
//   - In terms of Go, that means that nodes[:MirFaultyValidatorNumber] can be byzantine,
//     and nodes[MirFaultyValidatorNumber:] are honest nodes.

import (
	"context"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

const (
	MirTotalValidatorNumber  = 4 // N = 3F+1
	MirFaultyValidatorNumber = (MirTotalValidatorNumber - 1) / 3
	MirReferenceSyncingNode  = MirFaultyValidatorNumber // The first non-faulty node is a syncing node.
	MirHonestValidatorNumber = MirTotalValidatorNumber - MirFaultyValidatorNumber
	MirLearnersNumber        = MirFaultyValidatorNumber + 1
	TestedBlockNumber        = 10
	MaxDelay                 = 30
)

var mirTestOpts = []interface{}{kit.ThroughRPC(), kit.MirConsensus()}

func setupMangler(t *testing.T) {
	require.Greater(t, MirFaultyValidatorNumber, 0)
	require.Equal(t, MirTotalValidatorNumber, MirHonestValidatorNumber+MirFaultyValidatorNumber)

	err := mir.SetEnvManglerParams(200*time.Millisecond, 2*time.Second, 0)
	require.NoError(t, err)

	t.Cleanup(func() {
		err := os.Unsetenv(mir.ManglerEnv)
		require.NoError(t, err)
	})
}

// TestMirConsensusWithMangler tests that Mir operates normally when messaged are dropped or delayed.
func TestMirConsensusWithMangler(t *testing.T) {
	TestMirAllNodesMiningWithMangling(t)
	TestMirAllNodesMiningWithMessagingWithMangler(t)
	TestMirWhenLearnersJoinWithMangler(t)
}

func TestMirConsensusSmoke(t *testing.T) {
	TestMirOneNodeMining(t)
	TestMirAllNodesMining(t)
	TestMirStartStop(t)
	TestGenesisBlocksOfValidatorsAndLearners(t)
	TestMirFNodesNeverStart(t)
}

func TestMirConsensus(t *testing.T) {
	TestMirOneNodeMining(t)
	TestMirTwoNodesMining(t)
	TestMirAllNodesMining(t)
	TestGenesisBlocksOfValidatorsAndLearners(t)
	TestMirWhenLearnersJoin(t)
	TestMirNodesStartWithRandomDelay(t)
	TestMirFNodesNeverStart(t)
	TestMirFNodesStartWithRandomDelay(t)
	TestMirAllNodesMiningWithMessaging(t)
	TestMirWithFOmissionNodes(t)
	TestMirWithFCrashedNodes(t)
	TestMirWithFCrashedAndRecoveredNodes(t)
	TestMirStartStop(t)
	TestMirFNodesCrashLongTimeApart(t)
	TestMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash(t)
	TestMirFNodesSleepAndThenOperate(t)
}

// TestMirOneNodeMining tests that a Mir node can mine blocks.
func TestMirOneNodeMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	full, miner, ens := kit.EnsembleMinimalMir(t, mirTestOpts...)
	ens.BeginMirMining(ctx, &wg, miner)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, full)
	require.NoError(t, err)
}

// TestMirTwoNodesMining tests that two Mir nodes can mine blocks.
//
// NOTE: The peculiarity of this test is that it uses other mechanisms to instantiate testing
// comparing to the main tests here.
func TestMirTwoNodesMining(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	n1, n2, m1, m2, ens := kit.EnsembleTwoMirNodes(t, mirTestOpts...)

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

// TestMirAllNodesMining tests that n nodes can mine blocks normally.
func TestMirAllNodesMining(t *testing.T) {
	t.Run("TestMirAllNodesMining", func(t *testing.T) {
		var wg sync.WaitGroup

		ctx, cancel := context.WithCancel(context.Background())
		defer func() {
			t.Logf("[*] defer: cancelling %s context", t.Name())
			cancel()
			wg.Wait()
		}()

		nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
		ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

		err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
		require.NoError(t, err)
		err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
		require.NoError(t, err)
	})
}

// TestMirAllNodesMiningWithMangling run TestMirAllNodesMining with mangler.
func TestMirAllNodesMiningWithMangling(t *testing.T) {
	setupMangler(t)
	TestMirAllNodesMining(t)
}

// TestMirFNodesNeverStart tests that n − f nodes operate normally if f nodes never start.
func TestMirFNodesNeverStart(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirHonestValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

// TestMirWhenLearnersJoin tests that all nodes operate normally
// if new learner joins when the network is already started and syncs the whole network.
func TestMirWhenLearnersJoin(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Log(">>> learners join")

	var learners []*kit.TestFullNode
	for i := 0; i < MirLearnersNumber; i++ {
		var learner kit.TestFullNode
		ens.FullNode(&learner, kit.LearnerNode())
		require.Equal(t, true, learner.IsLearner())
		learners = append(learners, &learner)
	}

	ens.Start().InterconnectFullNodes()

	err = kit.AdvanceChain(ctx, TestedBlockNumber, learners...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], append(nodes[1:], learners...)...)
	require.NoError(t, err)
}

// TestMirWhenLearnersJoinWithMangler runs TestMirWhenLearnersJoin with mangler.
func TestMirWhenLearnersJoinWithMangler(t *testing.T) {
	setupMangler(t)
	TestMirWhenLearnersJoin(t)
}

// TestGenesisBlocksOfValidatorsAndLearners tests that genesis for validators and learners are correct.
func TestGenesisBlocksOfValidatorsAndLearners(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
	}()

	nodes, _, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.Bootstrapped()

	genesis, err := nodes[0].ChainGetGenesis(ctx)
	require.NoError(t, err)
	for i := range nodes[1:] {
		gen, err := nodes[i].ChainGetGenesis(ctx)
		require.NoError(t, err)
		require.Equal(t, genesis.String(), gen.String())
	}

	var learners []*kit.TestFullNode
	for i := 0; i < MirLearnersNumber; i++ {
		var learner kit.TestFullNode
		ens.FullNode(&learner, kit.LearnerNode()).Start()
		require.Equal(t, true, learner.IsLearner())
		learners = append(learners, &learner)
	}

	ens.Start()

	for i := range learners {
		gen, err := learners[i].ChainGetGenesis(ctx)
		require.NoError(t, err)
		require.Equal(t, genesis.String(), gen.String())
	}
}

// TestMirMessageFromLearner tests that messages can be sent from learners and validators,
// and successfully proposed by validators
func TestMirMessageFromLearner(t *testing.T) {
	t.Skip()
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	// immediately start learners
	var learners []*kit.TestFullNode
	for i := 0; i < MirLearnersNumber; i++ {
		var learner kit.TestFullNode
		ens.FullNode(&learner, kit.LearnerNode())
		require.Equal(t, true, learner.IsLearner())
		learners = append(learners, &learner)
	}

	ens.Start().InterconnectFullNodes()

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	// send funds to learners so they can send a message themselves
	for _, l := range learners {
		src, err := nodes[0].WalletDefaultAddress(ctx)
		require.NoError(t, err)
		dst, err := l.WalletDefaultAddress(ctx)
		require.NoError(t, err)

		t.Logf(">>> node %s is sending a message to node %s", src, dst)

		smsg, err := nodes[0].MpoolPushMessage(ctx, &types.Message{
			From:  src,
			To:    dst,
			Value: types.FromFil(10),
		}, nil)
		require.NoError(t, err)

		err = kit.MirNodesWaitMsg(ctx, smsg.Cid(), nodes...)
		require.NoError(t, err)
	}

	err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	for range learners {
		rand.Seed(time.Now().UnixNano())
		j := rand.Intn(len(learners))
		src, err := learners[j].WalletDefaultAddress(ctx)
		require.NoError(t, err)

		dst, err := learners[(j+1)%len(learners)].WalletDefaultAddress(ctx)
		require.NoError(t, err)

		t.Logf(">>> learner %s is sending a message to node %s", src, dst)

		smsg, err := learners[j].MpoolPushMessage(ctx, &types.Message{
			From:  src,
			To:    dst,
			Value: types.FromFil(1),
		}, nil)
		require.NoError(t, err)

		err = kit.MirNodesWaitMsg(ctx, smsg.Cid(), nodes...)
		require.NoError(t, err)

		// no message pending in message pool
		pend, err := learners[j].MpoolPending(ctx, types.EmptyTSK)
		require.NoError(t, err)
		require.Equal(t, len(pend), 0)
	}
}

// TestMirNodesStartWithRandomDelay tests that all nodes eventually operate normally
// if all nodes start with large, random delays (1-2 minutes).
func TestMirNodesStartWithRandomDelay(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMiningWithDelay(ctx, &wg, MaxDelay, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

// TestMirFNodesStartWithRandomDelay tests that all nodes eventually operate normally
// if f nodes start with large, random delays (1-2 minutes).
func TestMirFNodesStartWithRandomDelay(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMiningWithDelayForFaultyNodes(ctx, &wg, MaxDelay, miners[MirFaultyValidatorNumber:], miners[:MirFaultyValidatorNumber]...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)
	err = kit.CheckNodesInSync(ctx, 0, nodes[0], nodes[1:]...)
	require.NoError(t, err)
}

// TestMirAllNodesMiningWithMessaging tests that sending messages mechanism operates normally for all nodes when there are not any faults.
func TestMirAllNodesMiningWithMessaging(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
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

// TestMirAllNodesMiningWithMessagingWithMangler runs TestMirAllNodesMiningWithMessaging with mangler.
func TestMirAllNodesMiningWithMessagingWithMangler(t *testing.T) {
	setupMangler(t)
	TestMirAllNodesMiningWithMessaging(t)
}

// TestMirWithFOmissionNodes tests that n − f nodes operate normally and can recover
// if f nodes do not have access to network at the same time.
func TestMirWithFOmissionNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> disconnecting %d Mir miners", MirFaultyValidatorNumber)

	restoreConnections := ens.DisconnectMirMiners(miners[:MirFaultyValidatorNumber])

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> reconnecting %d Mir miners", MirFaultyValidatorNumber)
	restoreConnections()

	// err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	// require.NoError(t, err)
	time.Sleep(10 * time.Second)
	err = kit.CheckNodesInSync(ctx, 0, nodes[MirReferenceSyncingNode], nodes...)
	require.NoError(t, err)
}

// TestMirWithFCrashedNodes tests that n − f nodes operate normally and can recover
// if f nodes crash at the same time.
func TestMirWithFCrashedNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d miners", MirFaultyValidatorNumber)
	ens.CrashMirMiners(ctx, 0, miners[:MirFaultyValidatorNumber]...)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> restore %d miners", MirFaultyValidatorNumber)
	ens.RestoreMirMinersWithState(ctx, miners[:MirFaultyValidatorNumber]...)

	// FIXME: Consider using advance chain instead of a time.Sleep here if possible.
	// err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	// require.NoError(t, err)
	time.Sleep(10 * time.Second)
	err = kit.CheckNodesInSync(ctx, 0, nodes[MirReferenceSyncingNode], nodes...)
	require.NoError(t, err)
}

// TestMirStartStop tests that Mir nodes can be stopped.
func TestMirStartStop(t *testing.T) {
	t.Run("TestMirStartStop", func(t *testing.T) {
		var wg sync.WaitGroup
		wait := make(chan struct{})

		ctx, cancel := context.WithCancel(context.Background())
		defer func() {
			t.Logf("[*] defer: cancelling %s context", t.Name())
			cancel()
			select {
			case <-time.After(10 * time.Second):
				t.Fatalf("fail to stop Mir nodes")
			case <-wait:
			}
		}()

		go func() {
			// This goroutine is leaking after time.After(x) seconds with panicking.
			select {
			case <-time.After(200 * time.Second):
				panic("test time exceeded")
			case <-ctx.Done():
				return
			}
		}()

		go func() {
			// This goroutine is leaking after time.After(x) seconds with panicking.
			wg.Wait()
			close(wait)
		}()

		nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
		ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

		err := kit.AdvanceChain(ctx, 20, nodes...)
		require.NoError(t, err)
	})
}

// TestMirWithFCrashedAndRecoveredNodes tests that n − f nodes operate normally without significant interruption,
// and recovered nodes eventually operate normally
// if f nodes crash and then recover (with only initial state) after a long delay (few minutes).
func TestMirWithFCrashedAndRecoveredNodes(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d miners", MirFaultyValidatorNumber)
	ens.CrashMirMiners(ctx, 0, miners[:MirFaultyValidatorNumber]...)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> restore %d miners from scratch", MirFaultyValidatorNumber)
	ens.RestoreMirMinersWithEmptyState(ctx, miners[:MirFaultyValidatorNumber]...)

	err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Log(">>> checking nodes are in sync")
	err = kit.CheckNodesInSync(ctx, 0, nodes[MirReferenceSyncingNode], nodes...)
	require.NoError(t, err)
}

// TestMirFNodesCrashLongTimeApart tests that n − f nodes operate normally
// if f nodes crash, long time apart (few minutes).
func TestMirFNodesCrashLongTimeApart(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> crash %d nodes", MirFaultyValidatorNumber)
	ens.CrashMirMiners(ctx, MaxDelay, miners[:MirFaultyValidatorNumber]...)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Logf(">>> restore %d nodes", MirFaultyValidatorNumber)
	ens.RestoreMirMinersWithState(ctx, miners[:MirFaultyValidatorNumber]...)

	err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	err = kit.CheckNodesInSync(ctx, 0, nodes[MirReferenceSyncingNode], nodes...)
	require.NoError(t, err)
}

// TestMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash tests that n − f nodes operate normally
// and partitioned nodes eventually catch up
// if f nodes have a long period of no network access, but do not crash.
func TestMirFNodesHaveLongPeriodNoNetworkAccessButDoNotCrash(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		wg.Wait()
	}()

	nodes, miners, ens := kit.EnsembleMirNodes(t, MirTotalValidatorNumber, mirTestOpts...)
	ens.InterconnectFullNodes().BeginMirMining(ctx, &wg, miners...)

	err := kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	require.NoError(t, err)

	t.Logf(">>> disconnecting %d Mir miners", MirFaultyValidatorNumber)
	restoreConnections := ens.DisconnectMirMiners(miners[:MirFaultyValidatorNumber])

	t.Logf(">>> delay")
	kit.RandomDelay(MaxDelay)

	err = kit.ChainHeightCheckWithFaultyNodes(ctx, TestedBlockNumber, nodes[MirFaultyValidatorNumber:], nodes[:MirFaultyValidatorNumber]...)
	require.NoError(t, err)

	t.Log(">>> restoring network connections")
	restoreConnections()

	// FIXME: Consider using advance chain instead of a time.Sleep here if possible.
	// err = kit.AdvanceChain(ctx, TestedBlockNumber, nodes...)
	// require.NoError(t, err)
	time.Sleep(10 * time.Second)
	err = kit.CheckNodesInSync(ctx, 0, nodes[MirReferenceSyncingNode], nodes...)
	require.NoError(t, err)
}

// TestMirFNodesSleepAndThenOperate tests that n − f nodes operate normally without significant interruption
// and woken up nodes eventually operate normally
// if f  nodes sleep for a significant amount of time and then continue operating but keep network connection.
func TestMirFNodesSleepAndThenOperate(t *testing.T) {
	// TBD
	t.Skip()
}
