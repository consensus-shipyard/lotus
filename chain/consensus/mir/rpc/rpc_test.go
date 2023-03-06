package rpc

import (
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

func (t *TestService) Multiply(r *http.Request, req *TestServiceRequest, res *TestServiceResponse) error {
	res.Result = req.A * req.B
	return nil
}

func TestJSONRPCClient(t *testing.T) {
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
