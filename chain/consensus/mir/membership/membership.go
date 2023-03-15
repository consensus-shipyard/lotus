package membership

import (
	"github.com/multiformats/go-multiaddr"

	"github.com/consensus-shipyard/go-ipc-types/validator"

	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
)

type Reader interface {
	GetValidatorSet(subnet, tipset string) (*validator.Set, error)
}

var _ Reader = &FileMembership{}

type FileMembership struct {
	FileName string
}

func NewFileMembership(fileName string) FileMembership {
	return FileMembership{
		FileName: fileName,
	}
}

// GetValidatorSet gets the membership config from a file.
func (f FileMembership) GetValidatorSet(_, _ string) (*validator.Set, error) {
	return validator.NewValidatorSetFromFile(f.FileName)
}

// ------

var _ Reader = new(StringMembership)

type StringMembership string

// GetValidatorSet gets the membership config from the input string.
func (s StringMembership) GetValidatorSet(_, _ string) (*validator.Set, error) {
	return validator.NewValidatorSetFromString(string(s))
}

// -----
var _ Reader = new(EnvMembership)

type EnvMembership string

// GetValidatorSet gets the membership config from the input environment variable.
func (e EnvMembership) GetValidatorSet(_, _ string) (*validator.Set, error) {
	return validator.NewValidatorSetFromEnv(string(e))
}

// -----
var _ Reader = &OnChainMembership{}

type OnChainMembership struct {
	client rpc.JSONRPCRequestSender
}

func NewOnChainMembershipClient(client rpc.JSONRPCRequestSender) *OnChainMembership {
	return &OnChainMembership{
		client: client,
	}
}

// GetValidatorSet gets the membership config from the actor state.
func (c *OnChainMembership) GetValidatorSet(sunbet, tipset string) (*validator.Set, error) {
	req := struct {
		subnet string
		tipSet string
	}{
		subnet: sunbet,
		tipSet: tipset,
	}
	var set validator.Set
	err := c.client.SendRequest("ipc_queryValidatorSet", &req, &set)
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
