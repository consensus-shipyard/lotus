package mir

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	xerrors "golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/systems/smr"
	t "github.com/filecoin-project/mir/pkg/types"
	"github.com/filecoin-project/mir/pkg/util/maputil"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/types"
)

var _ smr.AppLogic = &StateManager{}

type Message []byte

type Batch struct {
	Messages   []Message
	Validators []t.NodeID
}

type StateManager struct {

	// Lotus API
	api v1api.FullNode

	// The current epoch number.
	currentEpoch t.EpochNr

	// For each epoch number, stores the corresponding membership.
	memberships map[t.EpochNr]map[t.NodeID]t.NodeAddress

	// Channel to send a membership.
	NewMembership chan map[t.NodeID]t.NodeAddress

	MirManager *Manager

	reconfigurationVotes map[t.EpochNr]map[string]int

	prevCheckpoint ParentMeta

	// Channel to send batches to Lotus.
	NextBatch chan *Batch

	// Channel to send checkpoints to Lotus
	NextCheckpoint chan *CheckpointData

	// Flag that determines if the mir is syncing.
	syncLk sync.Mutex
	synced bool // avoid accessing it directly, we have a lock and accessor methods
}

func NewStateManager(initialMembership map[t.NodeID]t.NodeAddress, m *Manager, api v1api.FullNode) (*StateManager, error) {
	// Initialize the membership for the first epochs.
	// We use configOffset+2 memberships to account for:
	// - The first epoch (epoch 0)
	// - The configOffset epochs that already have a fixed membership (epochs 1 to configOffset)
	// - The membership of the following epoch (configOffset+1) initialized with the same membership,
	//   but potentially replaced during the first epoch (epoch 0) through special configuration requests.
	memberships := make(map[t.EpochNr]map[t.NodeID]t.NodeAddress, ConfigOffset+2)
	for e := 0; e < ConfigOffset+2; e++ {
		memberships[t.EpochNr(e)] = initialMembership
	}

	sm := StateManager{
		NextBatch:            make(chan *Batch),
		NextCheckpoint:       make(chan *CheckpointData, 1),
		NewMembership:        make(chan map[t.NodeID]t.NodeAddress, 1),
		MirManager:           m,
		memberships:          memberships,
		currentEpoch:         0,
		reconfigurationVotes: make(map[t.EpochNr]map[string]int),
		api:                  api,
		synced:               false, // we assume mir is not synced, the first call is to RestoreState anyway.
	}

	// Initialize manager checkpoint state with the corresponding latest
	// checkpoint
	ch, err := sm.firstEpochCheckpoint()
	if err != nil {
		xerrors.Errorf("error getting checkpoint for epoch 0: %w", err)
	}
	c, err := ch.Cid()
	if err != nil {
		xerrors.Errorf("error getting cid for checkpoint: %w", err)
	}
	sm.prevCheckpoint = ParentMeta{Height: ch.Height, Cid: c}

	return &sm, nil
}

// RestoreState is called by Mir when the validator goes out-of-sync and it requires
// lotus to sync from the latest checkpoint. Mir provides lotus with the latest
// checkpoint and from this:
// - The latest membership and configuration for the consensus is recovered.
// - We clean all previous outdated checkpoints and configurations we may have
// received while trying to sync.
// - If there is a snapshot in the checkpoint, we poll our connections to sync
// to the latest block determined by the checkpoint.
// - We deliver the checkpoint to the mining process so it can be included in the next
// block (Mir provides the latest checkpoint, which hasn't been included in a block
// yet)
// - And we flag the mining process that we are synced and it can start accepting new
// batches from Mir and assembling new blocks.
func (sm *StateManager) RestoreState(checkpoint *checkpoint.StableCheckpoint) error {
	log.Debugf("Calling RestoreState from Mir for epoch %d", sm.currentEpoch)
	// if RestoreState is called is because Mir detected that we are not in sync.
	sm.unsetSynced()
	// release any previous checkpoint delivered and pending
	// to sync, as we are syncing again. This prevents a deadlock.
	sm.releaseNextCheckpointChan()

	config := checkpoint.Snapshot.Configuration
	sm.currentEpoch = t.EpochNr(config.EpochNr)
	sm.memberships = make(map[t.EpochNr]map[t.NodeID]t.NodeAddress, len(config.Memberships))

	for e, membership := range config.Memberships {
		// skew membership to current epoch, we are starting from a checkpoint
		sm.memberships[t.EpochNr(e)+sm.currentEpoch] = make(map[t.NodeID]t.NodeAddress)
		sm.memberships[t.EpochNr(e)+sm.currentEpoch] = t.Membership(membership)
	}

	newMembership := maputil.Copy(sm.memberships[t.EpochNr(config.EpochNr+ConfigOffset)])
	sm.memberships[t.EpochNr(config.EpochNr+ConfigOffset+1)] = newMembership

	// if mir provides a snapshot
	snapshot := checkpoint.Snapshot.AppData
	ch := &Checkpoint{}
	if len(snapshot) > 0 {
		// get checkpoint from snapshot.
		err := ch.FromBytes(snapshot)
		if err != nil {
			return xerrors.Errorf("error getting checkpoint from snapshot bytes: %w", err)
		}

		internalSync := false

		// From all the peers of my daemon try to get the latest tipset.
		connPeers, err := sm.api.NetPeers(sm.MirManager.ctx)
		if err != nil {
			return xerrors.Errorf("error getting list of peers from daemon: %w", err)
		}
		if len(connPeers) == 0 {
			return xerrors.Errorf("no connection with other filecoin peers, can't sync my daemon")
		}

		log.Debugf("Restoring from checkpoint at height %d ", ch.Height)
		for _, addr := range connPeers {
			log.Debugf("Trying to sync up to height %d from peer %s", ch.Height, addr.ID)
			ts, err := sm.api.SyncFetchTipSetFromPeer(sm.MirManager.ctx, addr.ID, types.NewTipSetKey(ch.BlockCids[0]))
			if err != nil {
				log.Errorf("error fetching latest tipset from peer %s: %w", addr.ID, err)
				continue
			}
			// wait for full-sync before returning from restoreState.
			err = sm.waitForBlock(ts.Height())
			if err != nil {
				return xerrors.Errorf("error waiting for next block %d: %w", ts.Height(), err)
			}
			internalSync = true
		}

		// if we couldn't find any valid peer or validator to sync from, just abort.
		if !internalSync {
			return xerrors.Errorf("couldn't find any good peers to sync from")
		}

		// once synced we deliver the checkpoint to our mining process so it can be
		// included in the next block (as the rest of Mir validators will do before
		// accepting the next batch), and we persist it locally.
		log.Debugf("Delivering checkpoint for height %d to mining process after sync", ch.Height)
		err = sm.deliverCheckpoint(checkpoint, ch)
		if err != nil {
			return xerrors.Errorf("error delivering checkpoint to lotus from mir after restoreState: %w", err)
		}
	}

	// flag the mining process that we are synced and we can start accepting new batches.
	sm.setSynced()
	return nil
}

// ApplyTXs applies transactions received from the availability layer to the app state.
func (sm *StateManager) ApplyTXs(txs []*requestpb.Request) error {
	var msgs []Message

	// For each request in the batch
	for _, req := range txs {
		switch req.Type {
		case TransportType:
			msgs = append(msgs, req.Data)
		case ReconfigurationType:
			err := sm.applyConfigMsg(req)
			if err != nil {
				return err
			}
		}
	}

	log.Debug("Sending new batch to assemble a lotus block")
	// Send a batch to the Lotus node.
	sm.NextBatch <- &Batch{
		Messages:   msgs,
		Validators: maputil.GetSortedKeys(sm.memberships[sm.currentEpoch]),
	}

	return nil
}

func (sm *StateManager) applyConfigMsg(in *requestpb.Request) error {
	newValSet := &ValidatorSet{}
	if err := newValSet.UnmarshalCBOR(bytes.NewReader(in.Data)); err != nil {
		return err
	}
	voted, err := sm.UpdateAndCheckVotes(newValSet)
	if err != nil {
		return err
	}
	if voted {
		err = sm.UpdateNextMembership(newValSet)
		if err != nil {
			return err
		}
	}
	return nil
}

func (sm *StateManager) NewEpoch(nr t.EpochNr) (map[t.NodeID]t.NodeAddress, error) {
	log.Debugf("current epoch triggered in new epoch: %d", sm.currentEpoch)
	// Sanity check.
	if nr != sm.currentEpoch+1 {
		return nil, xerrors.Errorf("expected next epoch to be %d, got %d", sm.currentEpoch+1, nr)
	}

	// The base membership is the last one membership.
	newMembership := maputil.Copy(sm.memberships[nr+ConfigOffset])

	// Append a new membership data structure to be modified throughout the new epoch.
	sm.memberships[nr+ConfigOffset+1] = newMembership

	// Update current epoch number.
	oldEpoch := sm.currentEpoch
	sm.currentEpoch = nr

	// Remove old membership.
	delete(sm.memberships, oldEpoch)
	delete(sm.reconfigurationVotes, oldEpoch)

	sm.NewMembership <- newMembership

	return newMembership, nil
}

func (sm *StateManager) UpdateNextMembership(valSet *ValidatorSet) error {
	_, mbs, err := validatorsMembership(valSet.GetValidators())
	if err != nil {
		return err
	}
	sm.memberships[sm.currentEpoch+ConfigOffset+1] = mbs
	return nil
}

// UpdateAndCheckVotes votes for the valSet and returns true if it has enough votes for this valSet.
func (sm *StateManager) UpdateAndCheckVotes(valSet *ValidatorSet) (bool, error) {
	h, err := valSet.Hash()
	if err != nil {
		return false, err
	}
	_, ok := sm.reconfigurationVotes[sm.currentEpoch]
	if !ok {
		sm.reconfigurationVotes[sm.currentEpoch] = make(map[string]int)
	}
	sm.reconfigurationVotes[sm.currentEpoch][string(h)]++
	votes := sm.reconfigurationVotes[sm.currentEpoch][string(h)]
	nodes := len(sm.memberships[sm.currentEpoch])

	if votes < weakQuorum(nodes) {
		return false, nil
	}
	return true, nil
}

// Snapshot is called by Mir every time a checkpoint period has
// passed and is time to create a new checkpoint. This function waits
// for the latest batch before the checkpoint to be synced is committed
// in our local state, and it collects the cids for all the blocks verified
// by the checkpoint.
func (sm *StateManager) Snapshot() ([]byte, error) {
	nextHeight := sm.prevCheckpoint.Height + sm.MirManager.GetCheckpointPeriod()
	log.Debugf("Mir requesting checkpoint snapshot for epoch %d and block height %d", sm.currentEpoch, nextHeight)
	log.Debugf("Previous checkpoint in snapshot: %v", sm.prevCheckpoint)

	// populating checkpoint template
	ch := Checkpoint{
		Height:    nextHeight,
		Parent:    sm.prevCheckpoint,
		BlockCids: make([]cid.Cid, 0),
	}

	// put blocks in descending order.
	i := nextHeight - 1

	// wait the last block to sync for the snapshot before
	// populating snapshot.
	log.Debugf("waiting for latest block (%d) before checkpoint to be synced to assemble the snapshot", i)
	err := sm.waitForBlock(i)
	if err != nil {
		return nil, xerrors.Errorf("error waiting for next block %d: %w", i, err)
	}

	for i >= sm.prevCheckpoint.Height {
		ts, err := sm.api.ChainGetTipSetByHeight(sm.MirManager.ctx, i, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("error getting tipset of height: %d: %w", i, err)
		}
		// In Mir tipsets have a single block, so we can access directly the block for
		// the tipset by accessing the first position.
		ch.BlockCids = append(ch.BlockCids, ts.Blocks()[0].Cid())
		i--
		log.Debugf("Getting Cid for block height %d and cid %s to include in snapshot", i, ts.Blocks()[0].Cid())
	}

	return ch.Bytes()
}

// Checkpoint is triggered by Mir when the committee agrees on the next checkpoint.
// We persist the checkpoint locally so we can restore from it after a restart
// or a crash and delivers it to the mining process to include it in the next
// block
// TODO: RestoreState and the persistence of the latest checkpoint locally may
// be redundant, we may be able to remove the latter.
func (sm *StateManager) Checkpoint(checkpoint *checkpoint.StableCheckpoint) error {
	// deserialize checkpoint data from Mir checkpoint to check that is the
	// right format.
	ch := &Checkpoint{}
	if err := ch.FromBytes(checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error getting checkpoint data from mir checkpoint: %w", err)
	}
	log.Debugf("Mir generated new checkpoint for height: %d", ch.Height)

	return sm.deliverCheckpoint(checkpoint, ch)
}

// deliver checkpoint receives a checkpoint, persists it locally in the local blockstore, and delivers
// it to the mining process to include it in a new block.
func (sm *StateManager) deliverCheckpoint(checkpoint *checkpoint.StableCheckpoint, snapshot *Checkpoint) error {
	// if we deserialized it correctly, we can persist it directly in the data store.
	if err := sm.MirManager.ds.Put(sm.MirManager.ctx, LatestCheckpointKey, checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// persist the protobuf of the snapshot to initialize mir from
	// snapshot if needed.
	b, err := checkpoint.Serialize()
	if err != nil {
		return xerrors.Errorf("error marshaling stablecheckpoint", err)
	}
	if err := sm.MirManager.ds.Put(sm.MirManager.ctx, LatestCheckpointPbKey, b); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// flush checkpoint by cid
	c, err := snapshot.Cid()
	if err != nil {
		return xerrors.Errorf("error computing cid for checkpoint: %w", err)
	}

	// store metadata for previous checkpoint in datastore and manager
	sm.prevCheckpoint = ParentMeta{Height: snapshot.Height, Cid: c}
	if err := sm.MirManager.ds.Put(sm.MirManager.ctx, datastore.NewKey(c.String()), checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// Send the checkpoint to Lotus and handle it there
	config := sm.MirManager.NewEpochConfigFromPb(checkpoint)
	log.Debug("Sending checkpoint to mining process to include in block")
	sm.NextCheckpoint <- &CheckpointData{*snapshot, checkpoint.Sn, *config}
	return nil
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
func (sm *StateManager) pollCheckpoint() *CheckpointData {
	select {
	case ch := <-sm.NextCheckpoint:
		log.Debugf("Polling checkpoint successful. Sending checkpoint for inclusion in block.")
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
	case <-sm.NextCheckpoint:
		return
	default:
		return
	}
}

// waitForBlock waits for the syncer to see as the head of the chain
// the block for the height determined as an input.
//
// The timeout to determine how much to wait before aborting is
// determined by the number of blocks to sync.
func (sm *StateManager) waitForBlock(height abi.ChainEpoch) error {
	// get base to determine the gap to sync and configure timeout.
	base, err := sm.api.ChainHead(sm.MirManager.ctx)
	if err != nil {
		return xerrors.Errorf("failed to get chain head: %w", err)
	}

	// one minute baseline timeout
	timeout := 60 * time.Second
	// add extra if the gap is big.
	if base.Height() < height {
		timeout = timeout + time.Duration(height-base.Height())*time.Second
	}
	log.Debugf("waiting for block on height %d with timeout %v", height, timeout)
	ctx, cancel := context.WithTimeout(sm.MirManager.ctx, timeout)
	defer cancel()

	out := make(chan bool, 1)
	go func() {
		head := abi.ChainEpoch(0)
		// poll until we get the desired height.
		// TODO: We may be able to add a slight sleep here if needed.
		for head != height {
			base, err := sm.api.ChainHead(sm.MirManager.ctx)
			if err != nil {
				log.Errorf("failed to get chain head: %w", err)
				return
			}
			head = base.Height()
			if head > height {
				log.Warnf("we already have a larger head. waiting %d, head %d", height, head)
				break
			}
		}
		out <- true
	}()

	select {
	case <-out:
		return nil
	case <-ctx.Done():
		return xerrors.Errorf("target block for checkpoint not reached after deadline")
	}
}

// get first checkpoint from genesis when a validator is restarted from scratch.
func (sm *StateManager) firstEpochCheckpoint() (*Checkpoint, error) {
	// if we are restarting the peer we may have something in the
	// mir database, if not let's return the genesis one.
	chb, err := sm.MirManager.ds.Get(sm.MirManager.ctx, LatestCheckpointKey)
	if err != nil {
		if err == datastore.ErrNotFound {
			genesis, err := sm.api.ChainGetGenesis(sm.MirManager.ctx)
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

func (sm *StateManager) setSynced() {
	sm.syncLk.Lock()
	defer sm.syncLk.Unlock()
	sm.synced = true
}

func (sm *StateManager) unsetSynced() {
	sm.syncLk.Lock()
	defer sm.syncLk.Unlock()
	sm.synced = true
}

func (sm *StateManager) isSynced() bool {
	sm.syncLk.Lock()
	defer sm.syncLk.Unlock()
	return sm.synced
}
