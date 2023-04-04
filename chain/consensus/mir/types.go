package mir

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/mir/pkg/checkpoint"
	"github.com/filecoin-project/mir/pkg/systems/trantor"
	mir "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
)

const (
	TransportRequest     = 1
	ConfigurationRequest = 0
)

type CtxCanceledWhileWaitingForBlockError struct {
	ID string
}

func (e CtxCanceledWhileWaitingForBlockError) Error() string {
	return fmt.Sprintf("validator %s context canceled while waiting for a snapshot", e.ID)
}

type ManglerParams struct {
	MinDelay time.Duration `json:"min_delay"`
	MaxDelay time.Duration `json:"max_delay"`
	DropRate float32       `json:"drop_rate"`
}

// SetEnvManglerParams sets Mir's mangler environment variable.
func SetEnvManglerParams(minDelay, maxDelay time.Duration, dropRate float32) error {
	p := ManglerParams{
		MinDelay: minDelay,
		MaxDelay: maxDelay,
		DropRate: dropRate,
	}
	s, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to encode mangle params: %w", err)
	}
	err = os.Setenv(ManglerEnv, string(s))
	if err != nil {
		return fmt.Errorf("failed to encode set mangler params: %w", err)
	}
	return nil
}

// GetEnvManglerParams gets Mir's mangler environment variable.
func GetEnvManglerParams() (ManglerParams, error) {
	mirManglerParams := os.Getenv(ManglerEnv)
	var p ManglerParams
	if err := json.Unmarshal([]byte(mirManglerParams), &p); err != nil {
		return ManglerParams{}, fmt.Errorf("failed to decode mangler params: %w", err)
	}
	return p, nil
}

var log = logging.Logger("mir-consensus")

// MirMessage interface that message types to be used in Mir need to implement.
type MirMessage interface {
	Serialize() ([]byte, error)
}

type MirMsgType int

const (
	ConfigMessageType = 0 // Mir specific config message
	SignedMessageType = 1 // Lotus signed message
)

func MsgType(m MirMessage) (MirMsgType, error) {
	switch m.(type) {
	case *types.SignedMessage:
		return SignedMessageType, nil
	default:
		return -1, fmt.Errorf("mir message type not implemented")

	}
}

func MessageBytes(msg MirMessage) ([]byte, error) {
	msgType, err := MsgType(msg)
	if err != nil {
		return nil, fmt.Errorf("unable to get msgType %w", err)
	}
	msgBytes, err := msg.Serialize()
	if err != nil {
		return nil, fmt.Errorf("unable to serialize message: %w", err)
	}
	return append(msgBytes, byte(msgType)), nil
}

type ParentMeta struct {
	Height abi.ChainEpoch
	Cid    cid.Cid
}

type VotedValidator struct {
	ID string
}

func (v VotedValidator) NodeID() mir.NodeID {
	return mir.NodeID(v.ID)
}

func NewVotedValidators(vs ...mir.NodeID) []VotedValidator {
	var validators []VotedValidator
	for _, v := range vs {
		validators = append(validators, VotedValidator{v.Pb()})
	}
	return validators
}

type VoteRecords struct {
	Records []VoteRecord
}

// VoteRecord states that VotedValidators voted for the validator set with ValSetHash having number ConfigurationNumber.
type VoteRecord struct {
	ConfigurationNumber uint64
	ValSetHash          string
	VotedValidators     []VotedValidator
}

type Checkpoint struct {
	// Height of the checkpoint
	Height abi.ChainEpoch
	// Cid of the blocks being committed in increasing order.
	// (index 0 is the first block of the range)
	BlockCids []cid.Cid
	// Parent checkpoint, i.e. metadata of previous checkpoint committed.
	Parent ParentMeta
	// The configuration number that can be accepted.
	NextConfigNumber uint64
}

func (ch *Checkpoint) isEmpty() bool {
	if ch.Height == 0 && ch.BlockCids == nil && (ch.Parent == ParentMeta{}) {
		return true
	}
	return false
}

func (ch *Checkpoint) Bytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := ch.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (ch *Checkpoint) FromBytes(b []byte) error {
	return ch.UnmarshalCBOR(bytes.NewReader(b))
}

func (ch *Checkpoint) Cid() (cid.Cid, error) {
	if ch.isEmpty() {
		return cid.Undef, nil
	}
	b, err := ch.Bytes()
	if err != nil {
		return cid.Undef, err
	}

	h, err := multihash.Sum(b, abi.HashFunction, -1)
	if err != nil {
		return cid.Undef, err
	}

	return cid.NewCidV1(abi.CidBuilder.GetCodec(), h), nil
}

func CertAsElectionProof(ch *checkpoint.StableCheckpoint) (*ltypes.ElectionProof, error) {
	b, err := ch.Certificate().Serialize()
	if err != nil {
		return nil, xerrors.Errorf("error serializing checkpoint data: %w", err)
	}
	return &ltypes.ElectionProof{WinCount: 0, VRFProof: b}, nil
}

// CheckpointAsVRFProof serializes the data from CheckpointData
// that is included for a checkpoint inside a Filecoin block.
func CheckpointAsVRFProof(ch *checkpoint.StableCheckpoint) (*ltypes.Ticket, error) {
	// we don´t include the certificate as it may be
	// different for each validator (leading blocks
	// with checkpoints from different validator to appear
	// completely different blocks).
	ch = ch.StripCert()
	b, err := ch.Serialize()
	if err != nil {
		return nil, xerrors.Errorf("error serializing checkpoint data: %w", err)
	}
	return &ltypes.Ticket{VRFProof: b}, nil
}

func CheckpointFromVRFProof(t *ltypes.Ticket) (*checkpoint.StableCheckpoint, error) {
	ch := &checkpoint.StableCheckpoint{}
	err := ch.Deserialize(t.VRFProof)
	if err != nil {
		return nil, xerrors.Errorf("error getting checkpoint data from VRF Proof: %w", err)
	}
	return ch, nil
}

func CertFromElectionProof(t *ltypes.ElectionProof) (*checkpoint.Certificate, error) {
	cert := &checkpoint.Certificate{}
	if err := cert.Deserialize(t.VRFProof); err != nil {
		return nil, xerrors.Errorf("error getting checkpoint certificate from ElectionProof: %w", err)
	}
	return cert, nil
}

func UnwrapCheckpointSnapshot(ch *checkpoint.StableCheckpoint) (*Checkpoint, error) {
	snap := &Checkpoint{}
	err := snap.FromBytes(ch.Snapshot.AppData)
	return snap, err
}

// GetCheckpointByHeight stable checkpoint by height from datastore.
func GetCheckpointByHeight(ctx context.Context, ds db.DB,
	height abi.ChainEpoch, params *trantor.Params) (*checkpoint.StableCheckpoint, error) {

	var (
		b   []byte
		err error
	)
	// if no height provided recover Mir from latest checkpoint
	if height <= 0 {
		b, err = ds.Get(ctx, LatestCheckpointPbKey)
		if err != nil {
			if err == datastore.ErrNotFound {
				if params != nil {
					return trantor.GenesisCheckpoint([]byte{}, *params)
				}
				return nil, xerrors.Errorf("no checkpoint for height %d or latest checkpoint found in db", height)
			}
			return nil, xerrors.Errorf("error getting latest checkpoint: %w", err)
		}
	} else {
		b, err = ds.Get(ctx, HeightCheckIndexKey(height))
		if err != nil {
			if err == datastore.ErrNotFound {
				return nil, xerrors.Errorf("no checkpoint peristed in database for height: %d", height)
			}
			return nil, xerrors.Errorf("error getting checkpoint for height %d: %w", height, err)
		}
	}

	ch := &checkpoint.StableCheckpoint{}
	err = ch.Deserialize(b)
	return ch, err
}

// CheckpointToFile persist Mir stable checkpoint on a file.
func CheckpointToFile(ch *checkpoint.StableCheckpoint, path string) error {
	b, err := ch.Serialize()
	if err != nil {
		return fmt.Errorf("error serializing checkpoint to persist in file: %s", err)
	}
	return serializedCheckToFile(b, path)
}

func serializedCheckToFile(b []byte, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0770); err != nil {
		return fmt.Errorf("error creating directory for checkpoint persistence: %s", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating file to persist checkpoint: %s", err)
	}
	_, err = file.Write(b)
	if err != nil {
		return fmt.Errorf("error writing checkpoint in file: %s", err)
	}
	return nil
}
