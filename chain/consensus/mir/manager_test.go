package mir

import (
	"context"
	"testing"
	"time"

	"github.com/consensus-shipyard/go-ipc-types/validator"
	golog "github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
)

type mockMembership struct {
	set *validator.Set
}

func (m mockMembership) GetMembershipInfo() (*membership.Info, error) {
	return &membership.Info{
		ValidatorSet: m.set,
	}, nil
}

func TestWaitForMembership(t *testing.T) {
	ctx := context.Background()

	s1 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy:10@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	v1, err := validator.NewValidatorFromString(s1)
	require.NoError(t, err)

	s2 := "t12zjpclnis2uytmcydrx7i5jcbvehs5ut3x6mvvq:10@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	v2, err := validator.NewValidatorFromString(s2)
	require.NoError(t, err)

	mb := mockMembership{validator.NewValidatorSet(0, []*validator.Validator{v1, v2})}

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
