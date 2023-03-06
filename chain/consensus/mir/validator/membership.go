package validator

import (
	"github.com/filecoin-project/lotus/chain/consensus/mir/rpc"
)

type FileMembership struct {
	FileName string
}

func NewFileMembership(fileName string) FileMembership {
	return FileMembership{
		FileName: fileName,
	}
}

// GetValidatorSet gets the membership config from a file.
func (f FileMembership) GetValidatorSet() (*Set, error) {
	return NewValidatorSetFromFile(f.FileName)
}

// ------

type StringMembership string

// GetValidatorSet gets the membership config from the input string.
func (s StringMembership) GetValidatorSet() (*Set, error) {
	return NewValidatorSetFromString(string(s))
}

// -----

type EnvMembership string

// GetValidatorSet gets the membership config from the input environment variable.
func (e EnvMembership) GetValidatorSet() (*Set, error) {
	return NewValidatorSetFromEnv(string(e))
}

// -----

type ActorMembership struct {
	client *rpc.JSONRPCClient
}

func NewActorMembershipClient(client *rpc.JSONRPCClient) *ActorMembership {
	return &ActorMembership{
		client: client,
	}
}

// GetValidatorSet gets the membership config from the actor state.
func (c *ActorMembership) GetValidatorSet() (*Set, error) {
	var set Set
	err := c.client.SendRequest("Filecoin.GetValidatorSet", nil, &set)
	if err != nil {
		return nil, err
	}
	return &set, err
}
