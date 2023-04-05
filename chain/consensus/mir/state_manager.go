package mir

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"time"

	"github.com/consensus-shipyard/go-ipc-types/validator"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/systems/trantor"
	t "github.com/filecoin-project/mir/pkg/types"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
	"github.com/filecoin-project/lotus/chain/consensus/mir/pool/fifo"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
)

var (
	LatestCheckpointKey   = datastore.NewKey("mir/latest-check")
	LatestCheckpointPbKey = datastore.NewKey("mir/latest-check-pb")

	PeerDiscoveryInterval   = 800 * time.Millisecond
	PeerDiscoveryTimeout    = 3 * time.Minute
	WaitForHeightMinTimeout = 30 * time.Second
)

type Message []byte

type Batch struct {
	Messages []Message
}

var _ trantor.AppLogic = &StateManager{}

type StateManager struct {
	// parent context
	ctx context.Context

	// Lotus API
	api v1api.FullNode

	// The current epoch number.
	currentEpoch t.EpochNr

	// For each epoch number, stores the corresponding membership.
	// It stores the current membership and the memberships of ConfigOffset following epochs.
	// It is updated by the NewEpoch function called by Mir on epoch transition (and on state transfer).
	memberships map[t.EpochNr]map[t.NodeID]t.NodeAddress

	// Next membership to return from NewEpoch.
	// Attention: No in-place modifications of this field are allowed.
	//            At reconfiguration, a new map with an updated membership must be assigned to this variable.
	nextNewMembership map[t.NodeID]t.NodeAddress

	confManager *ConfigurationManager

	ds db.DB

	requestPool *fifo.Pool

	// reconfigurationVotes implements ConfigurationNumber->ValSetHash->[]NodeID mapping.
	reconfigurationVotes map[uint64]map[string]map[t.NodeID]struct{}

	// nextConfigurationNumber is the acceptable configuration number.
	// The initial nextConfigurationNumber is 1.
	nextConfigurationNumber uint64

	prevCheckpoint ParentMeta

	checkpointRepo string // Path where checkpoints are (optionally) persisted

	// Channel to send checkpoints to assemble them in blocks.
	nextCheckpointChan chan *checkpoint.StableCheckpoint

	// Validator ID.
	id string

	// Mir chain height.
	height abi.ChainEpoch

	configOffset int
}

func NewStateManager(
	ctx context.Context,
	initialMembership map[t.NodeID]t.NodeAddress,
	cm *ConfigurationManager,
	api v1api.FullNode,
	ds db.DB,
	pool *fifo.Pool,
	cfg *Config,
) (*StateManager, error) {
	sm := StateManager{
		ctx:                     ctx,
		nextCheckpointChan:      make(chan *checkpoint.StableCheckpoint, 1),
		confManager:             cm,
		ds:                      ds,
		requestPool:             pool,
		currentEpoch:            0,
		api:                     api,
		id:                      cfg.Addr.String(),
		nextConfigurationNumber: 1,
		checkpointRepo:          cfg.CheckpointRepo,
		configOffset:            cfg.Consensus.ConfigOffset,
	}

	sm.reconfigurationVotes = sm.confManager.GetConfigurationVotes()

	// Initialize the membership for the first epoch and the ConfigOffset following ones (thus ConfigOffset+1).
	// Note that sm.memberships[0] will almost immediately be overwritten by the first call to NewEpoch.
	sm.memberships = make(map[t.EpochNr]map[t.NodeID]t.NodeAddress, sm.configOffset+1)
	for e := 0; e < sm.configOffset+1; e++ {
		sm.memberships[t.EpochNr(e)] = initialMembership
	}
	sm.nextNewMembership = initialMembership

	// Initialize manager checkpoint state with the corresponding latest
	// checkpoint
	ch, err := sm.firstEpochCheckpoint()
	if err != nil {
		return nil, xerrors.Errorf("validator %v failed to get checkpoint for epoch 0: %w", sm.id, err)
	}
	c, err := ch.Cid()
	if err != nil {
		return nil, xerrors.Errorf("validator %v failed to get cid for checkpoint: %w", sm.id, err)
	}
	sm.prevCheckpoint = ParentMeta{Height: ch.Height, Cid: c}

	return &sm, nil
}

// syncFromPeers sync the chain from Filecoin peers.
func (sm *StateManager) syncFromPeers(tsk types.TipSetKey) (err error) {
	log.With("validator", sm.id).Infof("syncFromPeers for TSK %s started", tsk)
	defer log.With("validator", sm.id).Infof("syncFromPeers for TSK %s finished", tsk)

	// From all the peers of my daemon try to get the latest tipset.
	timeout := time.After(PeerDiscoveryTimeout)
	heightTimeout := WaitForHeightMinTimeout
	attempt := time.NewTicker(PeerDiscoveryInterval)
	defer attempt.Stop()

	var connPeers []peer.AddrInfo
	for {

		connPeers, err = sm.api.NetPeers(sm.ctx)
		if err != nil {
			return xerrors.Errorf("failed to get peers syncing to TSK %s: %w", tsk, err)
		}
		if len(connPeers) == 0 {
			log.With("validator", sm.id).Warnf("syncFromPeers for TSK %s: no connected peers", tsk)
			// if we are the only validator, we can return was we don't need to sync from anyone.
			// This way we can restart a solo validator without the need of other nodes.
			if len(sm.memberships[sm.currentEpoch]) == 1 {
				return nil
			}
		}

		for _, p := range connPeers {
			ts, err := sm.api.SyncFetchTipSetFromPeer(sm.ctx, p.ID, tsk)
			if err != nil {
				log.With("validator", sm.id).Errorf("failed to get the latest tipset from peer %s: %v", p.ID, err)
				continue
			}

			// Wait for full-sync before returning from restoreState.
			// Here we use the timeout-based waitForWeight to be able to switch to another available peer if needed.
			// If we used timeout free function then we could choose a malicious node that has sent us an incorrect tipset.
			err = sm.waitForHeightWithTimeout(heightTimeout, ts.Height())
			if err != nil {
				log.With("validator", sm.id).Warnf("waitForHeightWithTimeout at %d error: %v", ts.Height(), err)
				continue
			}
			log.With("validator", sm.id).Infof("syncFromPeers for TSK %s completed via %v", tsk, p.ID)
			return nil
		}
		// Clear the list that will be updated on the next FOR step.
		connPeers = nil
		//
		heightTimeout += WaitForHeightMinTimeout

		select {
		case <-sm.ctx.Done():
			return xerrors.Errorf("syncFromPeers context cancelled")
		case <-timeout:
			return xerrors.Errorf("syncing from peers timeout exceeded")
		case <-attempt.C:
		}
	}
}

// RestoreState is called by Mir when the validator goes out-of-sync, and it requires
// lotus to sync from the latest checkpoint. Mir provides lotus with the latest
// checkpoint and from this:
// - The latest membership and configuration for the consensus is recovered.
// - We clean all previous outdated checkpoints and configurations we may have received while trying to sync.
// - If there is a snapshot in the checkpoint, we poll our connections to sync
// to the latest block determined by the checkpoint.
// - We deliver the checkpoint to the mining process, so it can be included in the next
// block (Mir provides the latest checkpoint, which hasn't been included in a block yet)
// - And we flag the mining process that we are synced, and it can start accepting new
// batches from Mir and assembling new blocks.
func (sm *StateManager) RestoreState(checkpoint *checkpoint.StableCheckpoint) error {
	log.With("validator", sm.id).Infof("RestoreState for epoch %d started", sm.currentEpoch)
	defer log.With("validator", sm.id).Infof("RestoreState for epoch %d finished", sm.currentEpoch)
	// release any previous checkpoint delivered and pending
	// to sync, as we are syncing again. This prevents a deadlock.
	sm.releaseNextCheckpointChan()

	config := checkpoint.Snapshot.EpochData.EpochConfig
	sm.currentEpoch = t.EpochNr(config.EpochNr)

	// Sanity check.
	if len(config.Memberships) != sm.configOffset+1 {
		return fmt.Errorf("validator %v checkpoint contains %d memberships, expected %d (ConfigOffset=%d)",
			sm.id, len(config.Memberships), sm.configOffset+1, sm.configOffset)
	}

	// Set memberships for the current epoch and ConfigOffset following ones.
	// Note that sm.memberships[i+sm.currentEpoch] will almost immediately be overwritten by the first call to NewEpoch.
	sm.memberships = make(map[t.EpochNr]map[t.NodeID]t.NodeAddress, len(config.Memberships))
	for i, mb := range config.Memberships {
		sm.memberships[t.EpochNr(i)+sm.currentEpoch] = t.Membership(mb)
	}

	// The next membership is the last known membership. It may be replaced by another one during this epoch.
	sm.nextNewMembership = sm.memberships[t.EpochNr(config.EpochNr+uint64(sm.configOffset))]
	log.With("validator", sm.id).Infof("RestoreState: next membership size is %d at epoch %d", len(sm.nextNewMembership), sm.currentEpoch)

	// if mir provides a snapshot
	snapshot := checkpoint.Snapshot.AppData
	var ch Checkpoint
	if len(snapshot) > 0 {
		// get checkpoint from snapshot.
		err := ch.FromBytes(snapshot)
		if err != nil {
			return xerrors.Errorf("%v failed to unmarshal checkpoint: %w", sm.id, err)
		}

		chCID, err := ch.Cid()
		if err != nil {
			return xerrors.Errorf("%v failed to get checkpoint CID: %w", sm.id, err)
		}

		log.With("validator", sm.id).Infof("Restoring state from checkpoint (%d, %v)", ch.Height, chCID)

		// Restore the height, and configuration number and configuration votes.
		sm.height = ch.Height - 1
		sm.nextConfigurationNumber = ch.NextConfigNumber

		// purge any state previous to the checkpoint
		if err = sm.api.SyncPurgeForRecovery(sm.ctx, ch.Height); err != nil {
			return xerrors.Errorf("%v couldn't purge state to recover from checkpoint: %w", sm.id, err)
		}

		if err = sm.syncFromPeers(types.NewTipSetKey(ch.BlockCids[0])); err != nil {
			return xerrors.Errorf("%v couldn't sync from peers for checkpoint (%d, %v): %w", sm.id, ch.Height, chCID, err)
		}
	} else {
		log.With("validator", sm.id).Infof("Snapshot len is zero")
	}

	return nil
}

// ApplyTXs applies transactions received from the availability layer to the app state
// and creates a Lotus block from the delivered batch.
func (sm *StateManager) ApplyTXs(txs []*requestpb.Request) error {
	log.With("validator", sm.id).Info("applytxs started")
	defer log.With("validator", sm.id).Info("applytxs finished")

	var mirMsgs []Message

	sm.height++

	// For each request in the batch
	for _, req := range txs {
		switch req.Type {
		case TransportRequest:
			mirMsgs = append(mirMsgs, req.Data)
		case ConfigurationRequest:
			err := sm.applyConfigMsg(req)
			if err != nil {
				return err
			}
		}
	}

	if err := sm.ctx.Err(); err != nil {
		return nil
	}

	base, err := sm.api.ChainGetTipSetByHeight(sm.ctx, sm.height-1, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("validator %v failed to get chain head: %w", sm.id, err)
	}
	log.With("validator", sm.id).Debugf("Trying to mine new block over base: %s", base.Key())

	msgs := sm.getSignedMessages(mirMsgs)
	log.With("validator", sm.id).With("epoch", sm.currentEpoch).
		With("height", sm.height).Infof("try to create a block: msgs - %d", len(msgs))

	// include checkpoint in VRF proof field?
	vrfCheckpoint := &ltypes.Ticket{VRFProof: nil}
	eproofCheckpoint := &ltypes.ElectionProof{}
	if ch := sm.pollCheckpoint(); ch != nil {
		eproofCheckpoint, err = CertAsElectionProof(ch)
		if err != nil {
			return xerrors.Errorf("validator %v failed to set eproof from checkpoint certificate: %w", sm.id, err)
		}
		vrfCheckpoint, err = CheckpointAsVRFProof(ch)
		if err != nil {
			return xerrors.Errorf("validator %v failed to set vrfproof from checkpoint: %w", sm.id, err)
		}
		log.With("validator", sm.id).Infof("Including Mir checkpoint for in block %d", sm.height)
	}

	bh, err := sm.api.MinerCreateBlock(sm.ctx, &lapi.BlockTemplate{
		// mir blocks are created by all miners. We use system actor as miner of the block
		Miner:            builtin.SystemActorAddr,
		Parents:          base.Key(),
		BeaconValues:     nil,
		Ticket:           vrfCheckpoint,
		Eproof:           eproofCheckpoint,
		Epoch:            sm.height,
		Timestamp:        uint64(sm.height),
		WinningPoStProof: nil,
		Messages:         msgs,
	})
	if err != nil {
		return xerrors.Errorf("validator %v failed to create a block: %w", sm.id, err)
	}
	if bh == nil {
		log.With("validator", sm.id).With("epoch", sm.height).Debug("created a nil block")
		return nil
	}

	err = sm.api.SyncSubmitBlock(sm.ctx, &types.BlockMsg{
		Header:        bh.Header,
		BlsMessages:   bh.BlsMessages,
		SecpkMessages: bh.SecpkMessages,
	})
	if err != nil {
		return xerrors.Errorf("validator %v unable to sync a block: %w", sm.id, err)
	}

	log.With("validator", sm.id).With("epoch", sm.currentEpoch).Infof("mined block %d : %v ", bh.Header.Height, bh.Header.Cid())

	return nil
}

func (sm *StateManager) applyConfigMsg(msg *requestpb.Request) error {
	var valSet validator.Set
	if err := valSet.UnmarshalCBOR(bytes.NewReader(msg.Data)); err != nil {
		return err
	}

	enoughVotes, err := sm.countVote(t.NodeID(msg.ClientId), &valSet)
	if err != nil {
		log.With("validator", sm.id).Errorf("failed to apply config message: %v", err)
		return nil
	}
	// If we get the configuration message we have sent then we remove it from the configuration request storage.
	if msg.ClientId == sm.id {
		_ = sm.confManager.Done(t.ReqNo(msg.ReqNo)) // nolint
	}
	if !enoughVotes {
		return nil
	}

	err = sm.updateNextMembership(&valSet)
	if err != nil {
		return xerrors.Errorf("validator %v failed to update membership: %w", sm.id, err)
	}

	sm.nextConfigurationNumber = valSet.ConfigurationNumber
	for n := range sm.reconfigurationVotes {
		if n < sm.nextConfigurationNumber {
			delete(sm.reconfigurationVotes, n)
		}
	}

	return nil
}

func (sm *StateManager) updateNextMembership(set *validator.Set) error {
	_, mbs, err := membership.Membership(set.GetValidators())
	if err != nil {
		return err
	}
	sm.nextNewMembership = mbs
	log.With("validator", sm.id).
		Infof("updateNextMembership: current epoch %d, config number %d, next membership size: %d",
			sm.currentEpoch, sm.nextConfigurationNumber, len(mbs))
	return nil
}

// countVotes count votes for the validator set and returns true if we have got enough votes for this valSet.
func (sm *StateManager) countVote(votingValidator t.NodeID, set *validator.Set) (bool, error) {
	if set.ConfigurationNumber < sm.nextConfigurationNumber {
		return false, xerrors.Errorf("validator %s sent outdated vote: received - %d, expected - %d",
			votingValidator, set.ConfigurationNumber, sm.nextConfigurationNumber)
	}

	if _, found := sm.memberships[sm.currentEpoch][votingValidator]; !found {
		return false, xerrors.Errorf("validator %s is not in the membership", votingValidator)
	}

	h, err := set.Hash()
	if err != nil {
		return false, err
	}

	if _, exist := sm.reconfigurationVotes[set.ConfigurationNumber]; !exist {
		sm.reconfigurationVotes[set.ConfigurationNumber] = make(map[string]map[t.NodeID]struct{})
	}

	if _, exist := sm.reconfigurationVotes[set.ConfigurationNumber][string(h)]; !exist {
		sm.reconfigurationVotes[set.ConfigurationNumber][string(h)] = make(map[t.NodeID]struct{})
	}

	// Prevent double voting.
	if _, voted := sm.reconfigurationVotes[set.ConfigurationNumber][string(h)][votingValidator]; voted {
		return false, xerrors.Errorf("validator %s has already voted for configuration %d", votingValidator, set.ConfigurationNumber)
	}

	sm.reconfigurationVotes[set.ConfigurationNumber][string(h)][votingValidator] = struct{}{}
	if err := sm.confManager.StoreConfigurationVotes(sm.reconfigurationVotes); err != nil {
		log.With("validator", sm.id).
			Error("countVote: failed to store votes in epoch %d: %w", sm.currentEpoch, err)
	}

	votes := len(sm.reconfigurationVotes[set.ConfigurationNumber][string(h)])
	nodes := len(sm.memberships[sm.currentEpoch])
	log.With("validator", sm.id).
		Infof("UpdateAndCheckVotes: valset number %d, epoch %d: votes %d, nodes %d",
			set.ConfigurationNumber, sm.currentEpoch, votes, nodes)

	// We must have f+1 votes at least.
	if votes < weakQuorum(nodes) {
		return false, nil
	}
	return true, nil
}

func (sm *StateManager) NewEpoch(nr t.EpochNr) (map[t.NodeID]t.NodeAddress, error) {
	log.With("validator", sm.id).Infof("New epoch started: updating %d to %d", sm.currentEpoch, nr)
	defer log.With("validator", sm.id).Infof("New epoch finished: updating %d to %d", sm.currentEpoch, nr)

	// Sanity check. Generally, the new epoch is always the current epoch plus 1.
	// At initialization and right after state transfer, sm.currentEpoch already has been initialized
	// to the current epoch number.
	if nr != sm.currentEpoch && nr != sm.currentEpoch+1 {
		return nil, xerrors.Errorf("validator %v expected next epoch to be %d or %d, got %d",
			sm.id, sm.currentEpoch, sm.currentEpoch+1, nr)
	}

	// Make the nextNewMembership (agreed upon during the previous epoch) the fixed membership
	// for the epoch nr+ConfigOffset and a new copy of it for further modifications during the new epoch.
	sm.memberships[nr+t.EpochNr(sm.configOffset)+1] = sm.nextNewMembership

	// Update current epoch number.
	sm.currentEpoch = nr

	// Garbage-collect previous membership and old voting data.
	// Note that at initialization and after state transfer, these entries do not exist.
	delete(sm.memberships, sm.currentEpoch-1)

	log.With("validator", sm.id).
		Debugf("New epoch result: current epoch %d, current membership size %d, next membership size: %d, height: %d",
			sm.currentEpoch, len(sm.memberships[sm.currentEpoch]), len(sm.nextNewMembership), sm.height)

	return sm.nextNewMembership, nil
}

// Snapshot is called by Mir every time a checkpoint period has
// passed and is time to create a new checkpoint. This function waits
// for the latest batch before the checkpoint to be synced is committed
// in our local state, and it collects the cids for all the blocks verified
// by the checkpoint.
func (sm *StateManager) Snapshot() ([]byte, error) {
	log.With("validator", sm.id).Infof("Snapshot for epoch %d started", sm.currentEpoch)
	defer log.With("validator", sm.id).Infof("Snapshot for epoch %d finished", sm.currentEpoch)

	if sm.currentEpoch == 0 {
		return nil, xerrors.Errorf("validator %v tried to make a snapshot in epoch %d", sm.id, sm.currentEpoch)
	}

	nextHeight := sm.height + 1
	log.With("validator", sm.id).Infof("Snapshot started: epoch - %d, height - %d", sm.currentEpoch, sm.height)

	// populating checkpoint template
	ch := Checkpoint{
		Height:           nextHeight,
		Parent:           sm.prevCheckpoint,
		BlockCids:        make([]cid.Cid, 0),
		NextConfigNumber: sm.nextConfigurationNumber,
	}

	// put blocks in descending order.
	i := nextHeight - 1

	// Wait the last block to sync for the snapshot before populating snapshot.
	log.With("validator", sm.id).Infof("waiting for latest block (%d) before checkpoint to be synced to assemble the snapshot", i)
	if err := sm.waitForHeight(i); err != nil {
		return nil, xerrors.Errorf("snapshot: validator %v failed to wait for next block %d: %w", sm.id, i, err)
	}

	for i >= sm.prevCheckpoint.Height {
		ts, err := sm.api.ChainGetTipSetByHeight(sm.ctx, i, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("snapshot: validator %v failed to get tipset of height: %d: %w", sm.id, i, err)
		}
		// In Mir tipsets have a single block, so we can access directly the block for
		// the tipset by accessing the first position.
		ch.BlockCids = append(ch.BlockCids, ts.Blocks()[0].Cid())
		log.With("validator", sm.id).Infof("Getting Cid for block height %d and cid %s to include in snapshot", i, ts.Blocks()[0].Cid())
		i--
	}

	b, err := ch.Bytes()
	if err != nil {
		return nil, xerrors.Errorf("snapshot: validator %v failed to serialize checkpoint: %w", sm.id, err)
	}
	log.With("validator", sm.id).Infof("Snapshot finished: epoch - %d, height - %d", sm.currentEpoch, sm.height)
	return b, nil
}

// Checkpoint is triggered by Mir when the committee agrees on the next checkpoint.
// We persist the checkpoint locally so we can restore from it after a restart
// or a crash and delivers it to the mining process to include it in the next block.
//
// TODO: RestoreState and the persistence of the latest checkpoint locally may
// be redundant, we may be able to remove the latter.
func (sm *StateManager) Checkpoint(checkpoint *checkpoint.StableCheckpoint) error {
	log.With("validator", sm.id).Infof("Checkpoint for epoch %d started", sm.currentEpoch)
	defer log.With("validator", sm.id).Infof("Checkpoint for epoch %d finished", sm.currentEpoch)
	// deserialize checkpoint data from Mir checkpoint to check that is the
	// right format.
	ch := &Checkpoint{}
	if err := ch.FromBytes(checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("validator %v failed to get checkpoint data from mir checkpoint: %w", sm.id, err)
	}
	log.With("validator", sm.id).Infof("Mir generated new checkpoint for height: %d", ch.Height)

	if err := sm.deliverCheckpoint(checkpoint, ch); err != nil {
		return xerrors.Errorf("validator %v failed to deliver checkpoint: %w", sm.id, err)
	}

	// Reset fifo between checkpoints to avoid requests getting stuck.
	// See https://github.com/consensus-shipyard/lotus/issues/28
	sm.requestPool.Purge()
	return nil
}

// deliver checkpoint receives a checkpoint, persists it locally in the local block store, and delivers
// it to the mining process to include it in a new block.
func (sm *StateManager) deliverCheckpoint(checkpoint *checkpoint.StableCheckpoint, snapshot *Checkpoint) error {
	log.With("validator", sm.id).Infof("deliverCheckpoint for height %d started", snapshot.Height)
	defer log.With("validator", sm.id).Infof("deliverCheckpoint for height %d finished", snapshot.Height)

	// if we deserialized it correctly, we can persist it directly in the data store.
	if err := sm.ds.Put(sm.ctx, LatestCheckpointKey, checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// persist the stable checkpoint to initialize mir from it if needed
	b, err := checkpoint.Serialize()
	if err != nil {
		return xerrors.Errorf("error marshaling stable checkpoint: %w", err)
	}
	// store latest checkpoint.
	if err := sm.ds.Put(sm.ctx, LatestCheckpointPbKey, b); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}
	// index checkpoints by epoch to enable Mir to start from a specific checkpoint if needed
	// (this is useful to perform catastrophic recoveries of the network).
	if err := sm.ds.Put(sm.ctx, HeightCheckIndexKey(snapshot.Height), b); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// also index checkpoint snapshots by cid
	c, err := snapshot.Cid()
	if err != nil {
		return xerrors.Errorf("error computing cid for checkpoint: %w", err)
	}
	sm.prevCheckpoint = ParentMeta{Height: snapshot.Height, Cid: c}

	// store metadata for previous snapshot in datastore and manager to
	// perform additional verifications
	if err := sm.ds.Put(sm.ctx, CidCheckIndexKey(c), checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// optionally persist the checkpoint in a file
	// (this is a best-effort process, if it fails we shouldn't kill the process)
	// in the future we could add a flag that makes persistence STRICT to notify
	// that this process should fail if persisting to file fails.
	if sm.checkpointRepo != "" {
		// wrapping it in a routine to take it out of the critical path.
		go func() {
			f := path.Join(sm.checkpointRepo, "checkpoint-"+snapshot.Height.String()+".chkp")
			if err := serializedCheckToFile(b, f); err != nil {
				log.Errorf("error persisting checkpoint for height %d in path %s: %s", snapshot.Height, f, err)
			}
		}()
	}

	// Send the checkpoint to Lotus and handle it there
	log.With("validator", sm.id).Debug("Sending checkpoint to mining process to include in block")
	sm.nextCheckpointChan <- checkpoint
	return nil
}

func (sm *StateManager) getSignedMessages(mirMsgs []Message) (msgs []*types.SignedMessage) {
	log.With("validator", sm.id).With("epoch", sm.currentEpoch).
		Infof("received a block with %d messages", len(mirMsgs))
	for _, tx := range mirMsgs {
		input, err := parseTx(tx)
		if err != nil {
			log.With("validator", sm.id).Error("unable to decode a message in Mir block:", err)
			continue
		}

		switch msg := input.(type) {
		case *types.SignedMessage:
			// batch being processed, remove from mpool
			found := sm.requestPool.DeleteRequest(msg.Cid(), msg.Message.Nonce)
			if !found {
				log.With("validator", sm.id).
					Debugf("unable to find a message with %v hash in our local fifo.Pool", msg.Cid())
				// TODO: If we try to remove something from the pool, we should remember that
				// we already tried to remove that to avoid adding as it may lead to a dead-lock.
				// FIFO should be updated because we don't have the support for in-flight supports.
				// continue
			}
			msgs = append(msgs, msg)
			log.With("validator", sm.id).Infof("got message: to=%s, nonce= %d", msg.Message.To, msg.Message.Nonce)
		default:
			log.With("validator", sm.id).Error("unknown message type in a block")
		}
	}
	return
}

func HeightCheckIndexKey(epoch abi.ChainEpoch) datastore.Key {
	return datastore.NewKey(CheckpointDBKeyPrefix + epoch.String())
}

func CidCheckIndexKey(c cid.Cid) datastore.Key {
	return datastore.NewKey(CheckpointDBKeyPrefix + c.String())
}

func maxFaulty(n int) int {
	// assuming n > 3f:
	//   return max f
	return (n - 1) / 3
}

func weakQuorum(n int) int {
	// assuming n > 3f:
	//   return min q: q > f
	return maxFaulty(n) + 1
}

// pollCheckpoint listens to new available checkpoints to be
// added in lotus blocks.
func (sm *StateManager) pollCheckpoint() *checkpoint.StableCheckpoint {
	select {
	case ch := <-sm.nextCheckpointChan:
		log.With("validator", sm.id).Debugf("Polling checkpoint successful. Sending checkpoint for inclusion in block.")
		return ch
	default:
		return nil
	}
}

// consume a value from a buffered channel to release
// it and support new values to be sent without blocking.
// (this is needed because Mir sometimes call RestoreData several
// times with outdated checkpoints before fully syncing)
func (sm *StateManager) releaseNextCheckpointChan() {
	select {
	case <-sm.nextCheckpointChan:
		return
	default:
		return
	}
}

func (sm *StateManager) waitForHeightWithTimeout(timeout time.Duration, height abi.ChainEpoch) error {
	log.With("validator", sm.id).Debugf("waitForHeight %v started", height)
	defer log.With("validator", sm.id).Debugf("waitForHeight %v finished", height)

	base, err := sm.api.ChainHead(sm.ctx)
	if err != nil {
		return xerrors.Errorf("failed to get chain head: %w", err)
	}

	if base.Height() < height {
		timeout += time.Duration(height-base.Height()) * time.Second
	}

	ctx, cancel := context.WithTimeout(sm.ctx, timeout)
	defer cancel()

	if err := WaitForHeight(ctx, height, sm.api); err != nil {
		return xerrors.Errorf("failed to wait for a block: %w", err)
	}
	return nil
}

func (sm *StateManager) waitForHeight(height abi.ChainEpoch) error {
	log.With("validator", sm.id).Infof("waitForHeight %v started", height)
	defer log.With("validator", sm.id).Infof("waitForHeight %v finished", height)

	if err := WaitForHeight(sm.ctx, height, sm.api); err != nil {
		return xerrors.Errorf("failed to wait for a block: %w", err)
	}
	return nil
}

// get first checkpoint from genesis when a validator is restarted from scratch.
func (sm *StateManager) firstEpochCheckpoint() (*Checkpoint, error) {
	// if we are restarting the peer we may have something in the
	// mir database, if not let's return the genesis one.
	chb, err := sm.ds.Get(sm.ctx, LatestCheckpointKey)
	if err != nil {
		if err == datastore.ErrNotFound {
			genesis, err := sm.api.ChainGetGenesis(sm.ctx)
			if err != nil {
				return nil, xerrors.Errorf("error getting genesis block: %w", err)
			}
			// return genesis checkpoint
			return &Checkpoint{
				Height:    1,                                                     // we assume the genesis has been verified, we start from 1
				Parent:    ParentMeta{Height: 0, Cid: genesis.Blocks()[0].Cid()}, // genesis checkpoint
				BlockCids: make([]cid.Cid, 0),
			}, nil
		}
		return nil, err
	}
	ch := &Checkpoint{}
	if err := ch.FromBytes(chb); err != nil {
		return nil, err
	}
	return ch, nil
}

func parseTx(tx []byte) (interface{}, error) {
	ln := len(tx)
	// This is very simple input validation to be protected against invalid messages.
	// TODO: Make this smarter.
	if ln <= 2 {
		return nil, fmt.Errorf("mir tx len %d is too small", ln)
	}

	var err error
	var msg interface{}

	// TODO: Consider taking it out to a MirMessageFromBytes function
	// into mir/types.go so that we have all msgType functionality in
	// the same place.
	lastByte := tx[ln-1]
	switch lastByte {
	case SignedMessageType:
		msg, err = types.DecodeSignedMessage(tx[:ln-1])
	case ConfigMessageType:
		return nil, fmt.Errorf("config message is not supported")
	default:
		err = fmt.Errorf("unknown message type %d", lastByte)
	}

	if err != nil {
		return nil, err
	}

	return msg, nil
}

// WaitForHeight waits for the syncer to see as the head of the chain the block for the height determined as an input.
func WaitForHeight(ctx context.Context, height abi.ChainEpoch, api v1api.FullNode) error {
	// get base to determine the gap to sync and configure timeout.
	if err := ctx.Err(); err != nil {
		return err
	}
	base, err := api.ChainHead(ctx)
	if err != nil {
		return err
	}
	head := base.Height()

	for {
		if head >= height {
			return nil
		}

		select {
		case <-ctx.Done():
			return xerrors.Errorf("context cancelled while waiting for height %v", height)
		default:
			base, err := api.ChainHead(ctx)
			if err != nil {
				return err
			}
			head = base.Height()
		}
	}
}
