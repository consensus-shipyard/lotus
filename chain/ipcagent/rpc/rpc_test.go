package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/stretchr/testify/require"

	"github.com/consensus-shipyard/go-ipc-types/validator"
)

type TestService struct{}

type TestServiceRequest struct {
	A int
	B int
}

type TestServiceResponse struct {
	O int
}

func (t *TestService) Multiply(_ *http.Request, req *TestServiceRequest, res *TestServiceResponse) error {
	res.O = req.A * req.B
	return nil
}

func TestClient(t *testing.T) {
	s := rpc.NewServer()
	s.RegisterCodec(json2.NewCodec(), "application/json")
	err := s.RegisterService(new(TestService), "")
	require.NoError(t, err)
	require.Equal(t, true, s.HasMethod("TestService.Multiply"))

	token := "token123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))

		s.ServeHTTP(w, r)
	}))
	defer srv.Close()

	c := NewJSONRPCClient(srv.URL, token)

	var resp *TestServiceResponse
	err = c.SendRequest("TestService.Multiply", &TestServiceRequest{A: 1, B: 4}, &resp)
	require.NoError(t, err)
	require.Equal(t, 4, resp.O)
}

func TestClientCompatibleWithIPCAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		obj := make(map[string]interface{})

		d := json.NewDecoder(r.Body)
		err := d.Decode(&obj)
		require.NoError(t, err)

		require.Equal(t, "2.0", obj["jsonrpc"])
		require.NotEqual(t, "", obj["id"])
		require.NotEqual(t, "", obj["params"])
		require.Equal(t, "ipc_queryValidatorSet", obj["method"])

		var result = `
{
	"jsonrpc": "2.0",
	"result": {
		"validator_set": {
			"configuration_number": 22,
			"validators": [{
					"addr": "f1cp4q4lqsdhob23ysywffg2tvbmar5cshia4rweq",
					"net_addr": "/ip4/127.0.0.1/tcp/38443/p2p/12D3KooWM4Z6tymWBUC9LQ7NNJ2RtzoakV1vDSyzehzC17Dpo367",
					"weight": "1"
				},
				{
					"addr": "f1akaouty2buxxwb46l27pzrhl3te2lw5jem67xuy",
					"net_addr": "/ip4/127.0.0.1/tcp/40315/p2p/12D3KooWD9DHVsaPvBN5H16aWZ9KDChyrDSKVCnZegsJguuwd76E",
					"weight": "0"
				}
			]
		}
	},
	"id": "5577006791947779410"
}
`
		_, err = fmt.Fprint(w, result)
		require.NoError(t, err)
	}))
	defer srv.Close()

	req := struct {
		subnet string
		tipSet string
	}{
		subnet: "/root/test",
		tipSet: "QmPK1s3pNYLi9ERiq3BDxKa3XosgWwFRQUydHUtz4YgpqB",
	}

	var resp *struct {
		ValidatorSet validator.Set `json:"validator_set"`
	}
	c := NewJSONRPCClient(srv.URL, "")
	err := c.SendRequest("ipc_queryValidatorSet", req, &resp)
	require.NoError(t, err)

	require.Equal(t, uint64(22), resp.ValidatorSet.ConfigurationNumber)
	require.Equal(t, 2, len(resp.ValidatorSet.Validators))
}
