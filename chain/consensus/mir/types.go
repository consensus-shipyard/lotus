package mir

import (
	"bytes"
	"fmt"

	t "github.com/filecoin-project/mir/pkg/types"
	cid "github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	// ConfigOffset is the number of epochs by which to delay configuration changes.
	// If a configuration is agreed upon in epoch e, it will take effect in epoch e + 1 + configOffset.
	ConfigOffset        = 2
	TransportType       = 0
	ReconfigurationType = 1
)

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

// Mir's checkpoint period is computed as the number of validators times the SegmentLength.
// In order to configure the initial checkpoint period close to a specific value, we need
// to set the SegmentLength for the SMR system accordingly. This function does this math
// for you.
func segmentForCheckpointPeriod(desiredPeriod int, membership map[t.NodeID]t.NodeAddress) (int, error) {
	segment := desiredPeriod / len(membership)
	if segment < 1 {
		return 0, fmt.Errorf("wrong checkpoint period: the minimum checkpoint allowed for this number of validators is %d", len(membership))
	}
	return segment, nil
}

type Checkpoint struct {
	// Height of the checkpoint
	Height abi.ChainEpoch
	// Cid of the blocks being committed in increasing order.
	// (index 0 is the first block of the range)
	BlockCids []cid.Cid
	// Parent checkpoint, i.e. previous checkpoint committed.
	Parent cid.Cid
}

func (ch *Checkpoint) Bytes() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := ch.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (ch *Checkpoint) Cid() (cid.Cid, error) {
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
