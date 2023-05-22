package membership

import (
	"os"
	"testing"

	"github.com/consensus-shipyard/go-ipc-types/validator"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
)

func TestMembership(t *testing.T) {
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

func TestStringMembershipInfo(t *testing.T) {
	s1 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"

	vs := StringMembership("0;" + s1)
	info, err := vs.GetMembershipInfo()
	require.NoError(t, err)
	require.Equal(t, uint64(0), info.MinValidators)
	require.Equal(t, uint64(0), info.ValidatorSet.ConfigurationNumber)
	require.Equal(t, 1, len(info.ValidatorSet.Validators))

	s2 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	s3 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"

	vs = StringMembership("3;" + s1 + "," + s2 + "," + s3)
	info, err = vs.GetMembershipInfo()
	require.NoError(t, err)
	require.Equal(t, uint64(0), info.MinValidators)
	require.Equal(t, uint64(3), info.ValidatorSet.ConfigurationNumber)
	require.Equal(t, 3, len(info.ValidatorSet.Validators))
}

func TestOnchainMembershipInfo(t *testing.T) {
	v1, err := validator.NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v2, err := validator.NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v3, err := validator.NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)

	// Reintroduce the address to avoid looping.
	gw, err := address.NewIDAddress(64)
	require.NoError(t, err)

	vs := validator.NewValidatorSet(0, []*validator.Validator{v1, v2, v3})
	require.Equal(t, 3, vs.Size())
	require.Equal(t, uint64(0), vs.GetConfigurationNumber())

	mb, err := NewSetMembershipMsg(gw, vs)
	require.NoError(t, err)

	require.True(t, IsConfigMsg(gw, &mb.Message))
}

func TestFileMembershipInfo(t *testing.T) {
	fileName := "_mb_test_file.tmp"
	t.Cleanup(func() {
		err := os.Remove(fileName)
		require.NoError(t, err)
	})

	v1, err := validator.NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v2, err := validator.NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v3, err := validator.NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)

	vs := validator.NewValidatorSet(0, []*validator.Validator{v1, v2, v3})
	require.Equal(t, 3, vs.Size())
	require.Equal(t, uint64(0), vs.GetConfigurationNumber())

	err = vs.Save(fileName)
	require.NoError(t, err)

	mb := FileMembership{
		FileName: fileName,
	}

	info, err := mb.GetMembershipInfo()
	require.NoError(t, err)
	require.Equal(t, uint64(0), info.MinValidators)
	require.Equal(t, uint64(0), info.ValidatorSet.ConfigurationNumber)
	require.Equal(t, 3, len(info.ValidatorSet.Validators))
}
