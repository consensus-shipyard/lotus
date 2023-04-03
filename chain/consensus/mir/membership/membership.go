package membership

import (
	"fmt"
	"strings"

	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/validator"
	"github.com/multiformats/go-multiaddr"

	t "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
)

const (
	FakeSource    string = "fake"
	StringSource  string = "string"
	FileSource    string = "file"
	OnChainSource string = "onchain"
)

func IsSourceValid(source string) error {
	switch strings.ToLower(source) {
	case FileSource:
		return nil
	case OnChainSource:
		return nil
	default:
		return fmt.Errorf("membership source %s noot supported", source)
	}
}

type Info struct {
	MinValidators uint64
	ValidatorSet  *validator.Set
}

type Reader interface {
	GetMembershipInfo() (*Info, error)
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
func (f FileMembership) GetMembershipInfo() (*Info, error) {
	vs, err := validator.NewValidatorSetFromFile(f.FileName)
	if err != nil {
		return nil, err
	}

	return &Info{
		ValidatorSet: vs,
	}, nil
}

// ------

var _ Reader = new(StringMembership)

type StringMembership string

// GetMembershipInfo gets the membership config from the input string.
func (s StringMembership) GetMembershipInfo() (*Info, error) {
	vs, err := validator.NewValidatorSetFromString(string(s))
	if err != nil {
		return nil, err
	}

	return &Info{
		ValidatorSet: vs,
	}, nil
}

// -----
var _ Reader = new(EnvMembership)

type EnvMembership string

// GetMembershipInfo gets the membership config from the input environment variable.
func (e EnvMembership) GetMembershipInfo() (*Info, error) {
	vs, err := validator.NewValidatorSetFromEnv(string(e))
	if err != nil {
		return nil, err
	}

	return &Info{
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

type AgentResponse struct {
	ValidatorSet  validator.Set `json:"validator_set"`
	MinValidators uint64        `json:"min_validators"`
}

// GetMembershipInfo gets the membership config from the actor state.
func (c *OnChainMembership) GetMembershipInfo() (*Info, error) {
	req := struct {
		Subnet string `json:"subnet"`
	}{
		Subnet: c.Subnet.String(),
	}

	var resp AgentResponse
	err := c.client.SendRequest("ipc_queryValidatorSet", &req, &resp)
	if err != nil {
		return nil, err
	}
	return &Info{
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
