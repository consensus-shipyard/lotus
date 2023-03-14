package tests

import (
	"context"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api/client"
)

func getAuthToken(id string) (string, error) {
	b, err := ioutil.ReadFile("../_data/" + id + "/token")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func Test_ChainGrows(t *testing.T) {
	ctx := context.Background()

	token, err := getAuthToken("0")
	require.NoError(t, err)

	headers := http.Header{"Authorization": []string{"Bearer " + string(token)}}

	c, closer, err := client.NewFullNodeRPCV1(ctx, "ws://127.0.0.1:1230"+"/rpc/v1", headers)
	require.NoError(t, err)

	head, err := c.ChainHead(ctx)
	require.NoError(t, err)

	require.Greater(t, head.Height(), abi.ChainEpoch(1))
	closer()
}
