package mir

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
)

func TestWaitForMembership(t *testing.T) {
	ctx := context.Background()
	s1 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	s2 := "t12zjpclnis2uytmcydrx7i5jcbvehs5ut3x6mvvq@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"

	mb := membership.StringMembership("0;" + s1 + "," + s2)

	info, nodes, err := waitForMembershipInfo(ctx, "testID", mb, 3*time.Second)
	require.Nil(t, info)
	require.Nil(t, nodes)
	require.ErrorIs(t, err, ErrWaitForMembershipTimeout)

	info, nodes, err = waitForMembershipInfo(ctx, "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy", mb, 3*time.Second)
	require.NotNil(t, info)
	require.NotNil(t, nodes)
	require.NoError(t, err)
}
