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
)

type TestService struct{}

type TestServiceRequest struct {
	A int
	B int
}

type TestServiceResponse struct {
	Result int
}

func (t *TestService) Multiply(_ *http.Request, req *TestServiceRequest, res *TestServiceResponse) error {
	res.Result = req.A * req.B
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
	require.Equal(t, 4, resp.Result)
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
		require.Equal(t, "TestService.Multiply", obj["method"])
		require.Equal(t, map[string]interface{}{"A": float64(1), "B": float64(4)}, obj["params"])

		result := "{\"jsonrpc\":\"2.0\",\"result\":{\"Result\":4},\"id\":5577006791947779410}"

		_, err = fmt.Fprintf(w, result)
		require.NoError(t, err)
	}))
	defer srv.Close()

	var resp *TestServiceResponse
	c := NewJSONRPCClient(srv.URL, "")
	err := c.SendRequest("TestService.Multiply", &TestServiceRequest{A: 1, B: 4}, &resp)
	require.NoError(t, err)
}
