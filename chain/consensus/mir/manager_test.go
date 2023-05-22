package mir

import (
	"context"
	"testing"
	"time"

	golog "github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
)

func TestWaitForMembership(t *testing.T) {
	ctx := context.Background()
	s1 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	s2 := "t12zjpclnis2uytmcydrx7i5jcbvehs5ut3x6mvvq@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"

	mb := membership.StringMembership("0;" + s1 + "," + s2)

	logger := golog.Logger("test-logger")

	info, nodes, err := waitForMembershipInfo(ctx, "not-existing-ID", mb, logger, 6*time.Second)
	require.ErrorIs(t, err, ErrWaitForMembershipTimeout)
	require.Nil(t, info)
	require.Nil(t, nodes)

	info, nodes, err = waitForMembershipInfo(ctx, "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy", mb, logger, 6*time.Second)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, nodes)
}
