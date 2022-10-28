package mir

import (
	"bytes"
	"context"
	"fmt"
	"time"

	cid "github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p-core/peer"
	xerrors "golang.org/x/xerrors"
	"google.golang.org/protobuf/proto"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	"github.com/filecoin-project/mir/pkg/pb/checkpointpb"
	"github.com/filecoin-project/mir/pkg/pb/commonpb"
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

// RestoreState restores the application's state to the one represented by the passed argument.
// The argument is a binary representation of the application state returned from Snapshot().
//
// RestoreState expects a checkpoint and only returns when it is fully sync. We can have here
// WaitSync function. It should also recover the configuration.
func (sm *StateManager) RestoreState(snapshot []byte, config *commonpb.EpochConfig) error {
	fmt.Println("====XXXXX== RESTORE STATE BEING CALLED")
	sm.currentEpoch = t.EpochNr(config.EpochNr)
	sm.memberships = make(map[t.EpochNr]map[t.NodeID]t.NodeAddress, len(config.Memberships))

	for e, membership := range config.Memberships {
		// skew membership to current epoch, we are starting from a checkpoint
		sm.memberships[t.EpochNr(e)+sm.currentEpoch] = make(map[t.NodeID]t.NodeAddress)
		sm.memberships[t.EpochNr(e)+sm.currentEpoch] = t.Membership(membership)
	}
	fmt.Println("=== current epoch in RestoreState", sm.currentEpoch)

	newMembership := maputil.Copy(sm.memberships[t.EpochNr(config.EpochNr+ConfigOffset)])
	sm.memberships[t.EpochNr(config.EpochNr+ConfigOffset+1)] = newMembership

	// if mir provides a snapshot
	if len(snapshot) > 0 {
		// TODO:
		// - Get checkpoint from snapshot
		// - Point to the rest of the validators as peerID to get the tipset.
		// - Inform incoming block to the syncer like in hello protocol
		// - Wait for block until the chain head is the one received in the snapshot.
		ch := Checkpoint{}
		err := ch.FromBytes(snapshot)
		if err != nil {
			return xerrors.Errorf("error getting checkpoint from snapshot bytes: %w", err)
		}

		synced := false
		// From all the peers of my daemon try to get the latest tipset.
		connPeers, err := sm.api.NetPeers(sm.MirManager.ctx)
		if err != nil {
			return xerrors.Errorf("error getting list of peers from daemon: %w", err)
		}
		if len(connPeers) == 0 {
			return xerrors.Errorf("no connection with other filecoin peers, can't sync my daemon")
		}
		for _, addr := range connPeers {
			fmt.Println("===== Restoring from checkpoint height", ch.Height)
			fmt.Println("===== Trying to fetch from peer", addr.ID)
			ts, err := sm.api.SyncFetchTipSetFromPeer(sm.MirManager.ctx, addr.ID, types.NewTipSetKey(ch.BlockCids[0]))
			if err != nil {
				log.Errorf("error fetching latest tipset from peer %s: %w", addr.ID, err)
				continue
			}
			// // TODO: Check if the peer has the latest tipset that we need, if not move to
			// // the next one.
			// if ts.Height() < ch.Height {
			// 	// the peero doesn't have the latest tipset I need, keep looking
			// 	continue
			// }
			err = sm.waitNextBlock(ts.Height())
			if err != nil {
				return xerrors.Errorf("error waiting for next block %d: %w", ts.Height(), err)
			}
			synced = true

		}
		// TODO: If I couldn't find the latst peers just kill.
		if !synced {
			return xerrors.Errorf("couldn't find any good peers to sync from")
		}

		// FIXME: Here we need to recover the previous checkpoint so that we can generate
		// in the next snapshot the previous checkpoint.
		// TODO: Clean this and de-duplicate from RestoreState
		fmt.Println("===== ")
		// if we deserialized it correctly, we can persist it directly in the data store.
		if err := sm.MirManager.ds.Put(sm.MirManager.ctx, LatestCheckpointKey, snapshot); err != nil {
			return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
		}

		// FIXME: Create checkpoint protobuf and persist it?
		// persist the protobuf of the snapshot to initialize mir from
		// snapshot if needed.
		// b, err := proto.Marshal((*checkpointpb.StableCheckpoint)(ch))
		// if err != nil {
		// 	return xerrors.Errorf("error marshaling stablecheckpoint", err)
		// }
		// if err := sm.MirManager.ds.Put(sm.MirManager.ctx, LatestCheckpointPbKey, b); err != nil {
		// 	return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
		// }

		fmt.Println(" ==== Flushing CID and persisting prev checkpoint")
		// flush checkpoint by cid
		c, err := ch.Cid()
		if err != nil {
			return xerrors.Errorf("error computing cid for checkpoint", err)
		}

		// store metadata for previous checkpoint in datastore and manager
		sm.prevCheckpoint = ParentMeta{Height: ch.Height, Cid: c}
		if err := sm.MirManager.ds.Put(sm.MirManager.ctx, datastore.NewKey(c.String()), snapshot); err != nil {
			return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
		}
		// // - Get the cid of the latest block in the checkpoint and get TipSetKey if we don't have it.
		// for id, addr := range parseMultiAddrFromMembership(sm.memberships[sm.currentEpoch]) {
		// 	fmt.Println("===== ids I have in RestoreState", id, sm.MirManager.MirNode.ID)
		// 	// do not try to fetch from self
		// 	if id == sm.MirManager.MirNode.ID {
		// 		continue
		// 	}
		// 	ts, err := sm.api.SyncFetchTipSetFromPeer(sm.MirManager.ctx, addr.ID, types.NewTipSetKey(ch.BlockCids[0]))
		// 	if err != nil {
		// 		return xerrors.Errorf("error fetching latest tipset from peer %s: %w", addr.ID, err)
		// 	}
		// 	err = sm.waitNextBlock(ts.Height())
		// 	if err != nil {
		// 		return xerrors.Errorf("error waiting for next block %d: %w", ts.Height(), err)
		// 	}

		// }
	}
	fmt.Println("==== leaving restoreState")

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
	fmt.Println("=== current epoch in new epoch", sm.currentEpoch)
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
// for the latest batch before the checkpoint to be synced and committed
// in our local state, and it collects the cids for all the blocks verified
// by the checkpoint.
func (sm *StateManager) Snapshot() ([]byte, error) {
	// return genesis checkpoint for the first snapshot.
	// if sm.currentEpoch == 0 {
	// 	fmt.Println("===== first checkpoint being called")
	// 	ch, err := sm.firstEpochCheckpoint()
	// 	if err != nil {
	// 		return nil, xerrors.Errorf("could not get first epoch checkpoint: %w", err)
	// 	}
	// 	return ch.Bytes()
	// }
	fmt.Println("=== current epoch in snapshot", sm.currentEpoch)
	fmt.Println("=== previous checkpoint height", sm.prevCheckpoint.Height)

	nextHeight := sm.prevCheckpoint.Height + sm.MirManager.GetCheckpointPeriod()
	log.Debugf("Mir requesting checkpoint snapshot for epoch %d and block height %d", sm.currentEpoch, nextHeight)
	fmt.Println("=== previous checkpoint in snapshot", sm.prevCheckpoint)
	// TODO: The previous checkpoint should be the same even after restart. If this is not the case
	// mir reaches a different snapshot when recovering and it gets stuck. Maybe we should remove the sm.prevcheckpoint
	// all along? Why do we need it? Ok! because we are chaining.
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
	err := sm.waitNextBlock(i)
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
		log.Debugf("Getting Cid for block %d and cid %s to include in snapshot", i, ts.Blocks()[0].Cid())
	}

	b, err := ch.Bytes()
	c, _ := ch.Cid()
	fmt.Println("==== leaving snapshot", c)
	return b, err
}

// Checkpoint is triggered by Mir when the committee agrees on the next checkpoint.
// We persist the checkpoint locally so we can restore from it after a restart
// or a crash and delivers it to the mining process to include it in the next
// block
func (sm *StateManager) Checkpoint(checkpoint *checkpoint.StableCheckpoint) error {
	// deserialize checkpoint data from Mir checkpoint to check that is the
	// right format.
	ch := &Checkpoint{}
	if err := ch.FromBytes(checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error getting checkpoint data from mir checkpoint: %w", err)
	}
	log.Debugf("Mir generated new checkpoint for height: %d", ch.Height)
	fmt.Println("==== calling checkpoint for height ch", ch.Height)

	// if we deserialized it correctly, we can persist it directly in the data store.
	if err := sm.MirManager.ds.Put(sm.MirManager.ctx, LatestCheckpointKey, checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// persist the protobuf of the snapshot to initialize mir from
	// snapshot if needed.
	b, err := proto.Marshal((*checkpointpb.StableCheckpoint)(checkpoint))
	if err != nil {
		return xerrors.Errorf("error marshaling stablecheckpoint", err)
	}
	if err := sm.MirManager.ds.Put(sm.MirManager.ctx, LatestCheckpointPbKey, b); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// flush checkpoint by cid
	c, err := ch.Cid()
	if err != nil {
		return xerrors.Errorf("error computing cid for checkpoint", err)
	}

	// store metadata for previous checkpoint in datastore and manager
	sm.prevCheckpoint = ParentMeta{Height: ch.Height, Cid: c}
	if err := sm.MirManager.ds.Put(sm.MirManager.ctx, datastore.NewKey(c.String()), checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error flushing latest checkpoint in datastore: %w", err)
	}

	// Send the checkpoint to Lotus and handle it there
	config := sm.MirManager.NewEpochConfigFromPb(checkpoint)
	log.Debug("Sending checkpoint to mining process to include in block")
	sm.NextCheckpoint <- &CheckpointData{*ch, checkpoint.Sn, *config}
	fmt.Println("==== leaving checkpoint", c)
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

func (sm *StateManager) pollCheckpoint() *CheckpointData {
	select {
	case ch := <-sm.NextCheckpoint:
		log.Debugf("Polling checkpoint successful. Sending checkpoint for inclusion in block.")
		return ch
	default:
		return nil
	}
}

func (sm *StateManager) waitNextBlock(height abi.ChainEpoch) error {
	// FIXME: DO NOT MERGE UNTIL YOU MAKE THIS TIMEOUT CONFIGURABLE.
	// REVIEWER, PLEASE FLAG THIS IF YOU SEE THIS COMMENT.
	// WAITNEXTBLOCK SHOULD NOW BE CALLED WAITFORBLOCK AND TARGET A
	// HEIGHT, IF THIS IS NOT THE CASE... SHOUT!!! AND GIT BLAME.
	fmt.Println(">>>> Waiting for block", height)
	ctx, cancel := context.WithTimeout(sm.MirManager.ctx, 60*time.Second)
	defer cancel()
	out := make(chan bool, 1)
	errCh := make(chan error, 1)
	go func() {
		head := abi.ChainEpoch(0)
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
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return xerrors.Errorf("target block for checkpoint not reached after deadline")
	}
}

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

func parseMultiAddrFromMembership(membership map[t.NodeID]t.NodeAddress) map[t.NodeID]*peer.AddrInfo {
	parsedAddrInfo := make(map[t.NodeID]*peer.AddrInfo)
	for nodeID, addr := range membership {
		info, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			// disregard addresses that are not multiaddr.
			continue
		}
		parsedAddrInfo[nodeID] = info
	}
	return parsedAddrInfo

}
