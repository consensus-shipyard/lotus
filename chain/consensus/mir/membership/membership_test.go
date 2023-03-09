package membership

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/consensus-shipyard/go-ipc-types/validator"
)

func TestCacheLen(t *testing.T) {
	v1, err := validator.NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v2, err := validator.NewValidatorFromString("t12zjpclnis2uytmcydrx7i5jcbvehs5ut3x6mvvq@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)

	ids, mb, err := Membership([]*validator.Validator{v1, v2})
	require.NoError(t, err)

	require.Equal(t, 2, len(ids))
	require.Equal(t, 2, len(mb))

	require.Equal(t, "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy",
		ids[0].Pb())
	require.Equal(t, "t12zjpclnis2uytmcydrx7i5jcbvehs5ut3x6mvvq",
		ids[1].Pb())

	require.Equal(t, "/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ",
		mb["t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy"].String())
	require.Equal(t, "/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ",
		mb["t12zjpclnis2uytmcydrx7i5jcbvehs5ut3x6mvvq"].String())
}
