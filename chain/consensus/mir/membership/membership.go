package membership

import (
	"github.com/multiformats/go-multiaddr"

	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/validator"

	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
)

type MembershipType int32

const (
	FakeType    MembershipType = 0
	StringType  MembershipType = 1
	FileType    MembershipType = 2
	OnChainType MembershipType = 3
)

type Reader interface {
	GetValidatorSet() (*validator.Set, error)
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
func (f FileMembership) GetValidatorSet() (*validator.Set, error) {
	return validator.NewValidatorSetFromFile(f.FileName)
}

// ------

var _ Reader = new(StringMembership)

type StringMembership string

// GetValidatorSet gets the membership config from the input string.
func (s StringMembership) GetValidatorSet() (*validator.Set, error) {
	return validator.NewValidatorSetFromString(string(s))
}

// -----
var _ Reader = new(EnvMembership)

type EnvMembership string

// GetValidatorSet gets the membership config from the input environment variable.
func (e EnvMembership) GetValidatorSet() (*validator.Set, error) {
	return validator.NewValidatorSetFromEnv(string(e))
}

// -----
var _ Reader = &OnChainMembership{}

type OnChainMembership struct {
	client rpc.JSONRPCRequestSender
	Subnet sdk.SubnetID
}

func NewOnChainMembershipClient(client rpc.JSONRPCRequestSender, subnet sdk.SubnetID) *OnChainMembership {
	return &OnChainMembership{
		client: client,
		Subnet: subnet,
	}
}

type AgentReponse struct {
	ValidatorSet validator.Set `json:"validator_set"`
}

// GetValidatorSet gets the membership config from the actor state.
func (c *OnChainMembership) GetValidatorSet() (*validator.Set, error) {
	req := struct {
		Subnet string `json:"subnet"`
	}{
		Subnet: c.Subnet.String(),
	}
	resp := AgentReponse{}

	err := c.client.SendRequest("ipc_queryValidatorSet", &req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.ValidatorSet, err
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
