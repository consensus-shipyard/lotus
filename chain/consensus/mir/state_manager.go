package mir

import (
	"bytes"
	"fmt"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/mir/pkg/pb/checkpointpb"
	"github.com/filecoin-project/mir/pkg/pb/commonpb"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/systems/smr"
	t "github.com/filecoin-project/mir/pkg/types"
	"github.com/filecoin-project/mir/pkg/util/maputil"
	cid "github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	xerrors "golang.org/x/xerrors"
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

	// Channel to send batches to Lotus.
	NextBatch chan *Batch

	// Channel to send checkpoints to Lotus
	NextCheckpoint chan *CheckpointData

	// Channel to signal that we need to wait for a checkpoint
	// before proposing the next block
	waitCheckpoint chan bool

	// Channel to send a membership.
	NewMembership chan map[t.NodeID]t.NodeAddress

	MirManager *Manager

	reconfigurationVotes map[t.EpochNr]map[string]int

	prevCheckpoint ParentMeta
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
		waitCheckpoint:       make(chan bool, 1),
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
	sm.currentEpoch = t.EpochNr(config.EpochNr)
	sm.memberships = make(map[t.EpochNr]map[t.NodeID]t.NodeAddress, len(config.Memberships))

	for e, membership := range config.Memberships {
		sm.memberships[t.EpochNr(e)] = make(map[t.NodeID]t.NodeAddress)
		sm.memberships[t.EpochNr(e)] = t.Membership(membership)
	}

	newMembership := maputil.Copy(sm.memberships[t.EpochNr(config.EpochNr+ConfigOffset)])
	sm.memberships[t.EpochNr(config.EpochNr+ConfigOffset+1)] = newMembership

	// TODO FIXME: Return only after we have received a signal by the syncer that we are fully synced.

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

// TODO: Snapshot is called by Mir to get the content for the state
// from the application included in a checkpoint.
// The snapshot is assigned the height of the next coming block, or the number
// of blocks is committing.
// Thus, Snapshot for height n, includes blocks 0... n-1.
// This snapshot info is what is included serialized in the checkpoint
// This data structure should include height, cid, BlockHash, ParentHash,
// and any other information used to validate blocks and restore the state.
//
// In the mir-validator syncing process, for a checkpoint period of 4 blocks, we
// deliver without checkpoints block 0,1,2,3 and then we wait for a checkpoint
// and include it in block 4. When block 4 is delivered with the checkpoint, we
// can verify blocks 0,1,2,3 and execute them.
func (sm *StateManager) Snapshot() ([]byte, error) {
	if sm.currentEpoch == 0 {
		ch, err := sm.firstEpochCheckpoint()
		if err != nil {
			return nil, xerrors.Errorf("could not get first epoch checkpoint: %w", err)
		}
		return ch.Bytes()
	}

	// signal to wait for next checkpoint
	sm.waitCheckpoint <- true

	nextHeight := sm.prevCheckpoint.Height + sm.MirManager.GetCheckpointPeriod()
	ch := Checkpoint{
		Height:    nextHeight,
		Parent:    sm.prevCheckpoint,
		BlockCids: make([]cid.Cid, 0),
	}
	fmt.Println(">>>>>>>>>>>>>>>>>> Requesting snapshot for epoch", sm.currentEpoch)

	// return genesis checkpoint for the first snapshot.

	i := sm.prevCheckpoint.Height
	for i <= nextHeight-1 {
		ts, err := sm.api.ChainGetTipSetByHeight(sm.MirManager.ctx, i, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("error getting tipset of height: %d: %w", i, err)
		}
		fmt.Println(">>>>>>>>>>>>>>>>>> Getting cid for block", i)
		fmt.Println(">>>> Getting blocks cid for height", i, ts.Blocks())
		// In Mir tipsets have a single block, so we can access directly the block for
		// the tipset by accessing the first position.
		ch.BlockCids = append(ch.BlockCids, ts.Blocks()[0].Cid())
		i++
	}

	return ch.Bytes()
}

// This is called after check_period blocks have been produced, Mir creates a new
// checkpoint from these blocks and calls this. It will call Checkpoint immediately
// after snapshot to provide you with the certificate before creating new blocks.
// StableCheckpoint includes the snapshot agreed upon by all validators and the corresponding
// certificate.
//
// To verify a checkpoint we need:
// Instance of the cryptomodule and verify the signatures of the certificate.
// This can be used for the consensus validation.
// Verify that there are f+1 signature in the certificate.
// From there, we iterate and verify all signatures.
// 3f+1 number of nodes to tolerate f failures. f+1 for the signature to be correct.
func (sm *StateManager) Checkpoint(checkpoint *checkpointpb.StableCheckpoint) error {
	// deserialize checkpoint data from Mir checkpoint to check that is the
	// right format.
	ch := &Checkpoint{}
	if err := ch.FromBytes(checkpoint.Snapshot.AppData); err != nil {
		return xerrors.Errorf("error getting checkpoint data from mir checkpoint: %w", err)
	}

	// if we deserialized it correctly, we can persist it directly in the data store.
	if err := sm.MirManager.ds.Put(sm.MirManager.ctx, LasestCheckpointKey, checkpoint.Snapshot.AppData); err != nil {
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
	config := NewEpochConfigFromPb(checkpoint)
	sm.NextCheckpoint <- &CheckpointData{ch, checkpoint.Sn, config}
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
	// signalled that we need to wait for a checkpoint
	case <-sm.waitCheckpoint:
		fmt.Println(">>>>>>>>> Waiting for checkpoint")
		ch := <-sm.NextCheckpoint
		// sanity-check: verify signature. FIXME: remove from here
		fmt.Println("===== VERIFYING CERTIFICATE: ", sm.MirManager.CryptoManager.VerifyCheckpointCert(ch))
		return ch
	default:
		return nil
	}
}

func (sm *StateManager) firstEpochCheckpoint() (*Checkpoint, error) {
	// if we are restarting the peer we may have something in the
	// mir database, if not let's return the genesis one.
	chb, err := sm.MirManager.ds.Get(sm.MirManager.ctx, LasestCheckpointKey)
	if err != nil {
		if err == datastore.ErrNotFound {
			genesis, err := sm.api.ChainGetGenesis(sm.MirManager.ctx)
			if err != nil {
				return nil, xerrors.Errorf("error getting genesis block: %w", err)
			}
			// return genesis checkpoint
			return &Checkpoint{
				Height:    0,
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
