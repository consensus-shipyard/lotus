package validator

import (
	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
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
func (c *ActorMembership) GetValidatorSet() (*Set, error) {
	var set Set
	err := c.client.SendRequest("ipc_queryValidatorSet", nil, &set)
	if err != nil {
		return nil, err
	}
	return &set, err
}
