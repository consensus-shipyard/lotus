package kit

import (
	"encoding/json"

	"github.com/consensus-shipyard/go-ipc-types/validator"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
)

var _ rpc.JSONRPCRequestSender = &StubJSONRPCClient{}

type StubJSONRPCClient struct {
	nextSet string
}

func NewStubJSONRPCClient() *StubJSONRPCClient {
	return &StubJSONRPCClient{}
}

func (c *StubJSONRPCClient) SendRequest(method string, params interface{}, reply interface{}) error {
	set, err := validator.NewValidatorSetFromString(c.nextSet)
	if err != nil {
		return err
	}
	b, err := json.Marshal(set)
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, reply)
	if err != nil {
		return err
	}
	return nil
}
