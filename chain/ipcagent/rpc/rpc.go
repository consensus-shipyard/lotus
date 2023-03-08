package rpc

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	rpc "github.com/gorilla/rpc/v2/json2"
)

type JSONRPCRequestSender interface {
	SendRequest(method string, params interface{}, reply interface{}) error
}

var _ JSONRPCRequestSender = &JSONRPCClient{}

type JSONRPCClient struct {
	ctx   context.Context
	token string
	url   string
}

func NewJSONRPCClient(url, token string) *JSONRPCClient {
	return &JSONRPCClient{
		ctx:   context.Background(),
		token: token,
		url:   url,
	}
}

// NewInsecureJSONRPCClient creates a JSON RPC client with empty credentials.
func NewInsecureJSONRPCClient(url string) *JSONRPCClient {
	return &JSONRPCClient{
		ctx:   context.Background(),
		token: "",
		url:   url,
	}
}

func NewJSONRPCClientWithContext(ctx context.Context, url, token string) *JSONRPCClient {
	return &JSONRPCClient{
		ctx:   ctx,
		token: token,
		url:   url,
	}
}

func (c *JSONRPCClient) SendRequest(method string, params interface{}, reply interface{}) error {
	paramBytes, err := rpc.EncodeClientRequest(method, params)
	if err != nil {
		return fmt.Errorf("failed to encode client params: %w", err)
	}

	req, err := http.NewRequestWithContext(
		c.ctx,
		"POST",
		c.url,
		bytes.NewBuffer(paramBytes),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to issue request: %w", err)
	}

	// Return an error for any nonsuccessful status code
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return fmt.Errorf("received status code: %d", resp.StatusCode)
	}

	if err := rpc.DecodeClientResponse(resp.Body, reply); err != nil {
		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return fmt.Errorf("failed to decode client response: %w", err)
	}
	return resp.Body.Close()
}
