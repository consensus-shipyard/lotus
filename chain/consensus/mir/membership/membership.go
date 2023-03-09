package membership

import (
	"github.com/multiformats/go-multiaddr"

	"github.com/consensus-shipyard/go-ipc-types/validator"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
	t "github.com/filecoin-project/mir/pkg/types"
)

type Reader interface {
	GetValidatorSet() (*validator.Set, error)
}

type FileMembership struct {
	FileName string
}

func NewFileMembership(fileName string) FileMembership {
	return FileMembership{
		FileName: fileName,
	}
}

// GetValidatorSet gets the membership config from a file.
func (f FileMembership) GetValidatorSet() (*validator.Set, error) {
	return validator.NewValidatorSetFromFile(f.FileName)
}

// ------

type StringMembership string

// GetValidatorSet gets the membership config from the input string.
func (s StringMembership) GetValidatorSet() (*validator.Set, error) {
	return validator.NewValidatorSetFromString(string(s))
}

// -----

type EnvMembership string

// GetValidatorSet gets the membership config from the input environment variable.
func (e EnvMembership) GetValidatorSet() (*validator.Set, error) {
	return validator.NewValidatorSetFromEnv(string(e))
}

// -----

const JSONRPCServerURL = "http://127.0.0.1:3030"

type ActorMembership struct {
	client rpc.JSONRPCRequestSender
}

func NewActorMembershipClient(client rpc.JSONRPCRequestSender) *ActorMembership {
	return &ActorMembership{
		client: client,
	}
}

// GetValidatorSet gets the membership config from the actor state.
func (c *ActorMembership) GetValidatorSet() (*validator.Set, error) {
	var set validator.Set
	err := c.client.SendRequest("ipc_queryValidatorSet", nil, &set)
	if err != nil {
		return nil, err
	}
	return &set, err
}

// ----

// Membership validates that validators addresses are correct multi-addresses and
// returns all the corresponding IDs and map between these IDs and the multi-addresses.
func Membership(validators []*validator.Validator) ([]t.NodeID, map[t.NodeID]t.NodeAddress, error) {
	var nodeIDs []t.NodeID
	nodeAddrs := make(map[t.NodeID]t.NodeAddress)

	for _, v := range validators {
		id := t.NodeID(v.ID())
		a, err := multiaddr.NewMultiaddr(v.NetAddr)
		if err != nil {
			return nil, nil, err
		}
		nodeIDs = append(nodeIDs, id)
		nodeAddrs[id] = a
	}

	return nodeIDs, nodeAddrs, nil
}
