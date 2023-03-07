package kit

import (
	"encoding/json"

	"github.com/filecoin-project/lotus/chain/consensus/mir/rpc"
	"github.com/filecoin-project/lotus/chain/consensus/mir/validator"
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
