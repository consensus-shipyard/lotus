package membership

import (
	"github.com/multiformats/go-multiaddr"

	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/validator"

	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
)

type MembershipInfo struct {
	MinValidators uint64
	ValidatorSet  *validator.Set
}

type Reader interface {
	GetMembershipInfo() (*MembershipInfo, error)
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

// GetMembershipInfo gets the membership config from a file.
func (f FileMembership) GetMembershipInfo() (*MembershipInfo, error) {
	vs, err := validator.NewValidatorSetFromFile(f.FileName)
	if err != nil {
		return nil, err
	}

	return &MembershipInfo{
		ValidatorSet: vs,
	}, nil
}

// ------

var _ Reader = new(StringMembership)

type StringMembership string

// GetMembershipInfo gets the membership config from the input string.
func (s StringMembership) GetMembershipInfo() (*MembershipInfo, error) {
	vs, err := validator.NewValidatorSetFromString(string(s))
	if err != nil {
		return nil, err
	}

	return &MembershipInfo{
		ValidatorSet: vs,
	}, nil
}

// -----
var _ Reader = new(EnvMembership)

type EnvMembership string

// GetMembershipInfo gets the membership config from the input environment variable.
func (e EnvMembership) GetMembershipInfo() (*MembershipInfo, error) {
	vs, err := validator.NewValidatorSetFromEnv(string(e))
	if err != nil {
		return nil, err
	}

	return &MembershipInfo{
		ValidatorSet: vs,
	}, nil
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
	ValidatorSet  validator.Set `json:"validator_set"`
	MinValidators uint64        `json:"min_validators"`
}

// GetMembershipInfo gets the membership config from the actor state.
func (c *OnChainMembership) GetMembershipInfo() (*MembershipInfo, error) {
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
	return &MembershipInfo{
		ValidatorSet:  &resp.ValidatorSet,
		MinValidators: resp.MinValidators,
	}, nil
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
