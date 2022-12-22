package validator

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
)

func TestValidatorsFromString(t *testing.T) {
	sAddr := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy"
	v, err := NewValidatorFromString(sAddr + "@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	addr, err := address.NewFromString(sAddr)
	require.NoError(t, err)
	require.Equal(t, addr, v.Addr)
}

func TestMembership(t *testing.T) {
	v1, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v2, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v3, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)

	vs1 := NewValidatorSet([]Validator{v1, v2, v3})
	_, _, err = Membership(vs1.Validators)
	require.NoError(t, err)
}

func TestValidatorSetFromString(t *testing.T) {
	fileName := "_vs_test_file.tmp"
	t.Cleanup(func() {
		os.Remove(fileName) // nolint
	})

	v1, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v2, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v3, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)

	vs1 := NewValidatorSet([]Validator{v1, v2, v3})
	require.Equal(t, 3, vs1.Size())

	err = vs1.StoreToFile(fileName)
	require.NoError(t, err)

	vs2, err := NewValidatorSetFromFile(fileName)
	require.NoError(t, err)

	require.Equal(t, true, vs1.Equal(vs2))
}

func TestValidatorsToFile(t *testing.T) {
	fileName := "_v_test_file_.tmp"
	t.Cleanup(func() {
		os.Remove(fileName) // nolint
	})

	v1, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	err = v1.AppendToFile(fileName)
	require.NoError(t, err)

	v2, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	err = v2.AppendToFile(fileName)
	require.NoError(t, err)

	v3, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	err = v3.AppendToFile(fileName)
	require.NoError(t, err)

	vs1 := NewValidatorSet([]Validator{v1, v2, v3})
	require.Equal(t, 3, vs1.Size())

	vs2, err := NewValidatorSetFromFile(fileName)
	require.NoError(t, err)

	require.Equal(t, true, vs1.Equal(vs2))
}
