package validator

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
)

func TestHashIsEqualForEqualMemberships(t *testing.T) {
	v1, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)
	v2, err := NewValidatorFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ")
	require.NoError(t, err)

	vs1 := NewValidatorSet(0, []Validator{v1, v2})
	h1, err := vs1.Hash()
	require.NoError(t, err)
	vs2 := NewValidatorSet(0, []Validator{v2, v1})
	h2, err := vs2.Hash()
	require.NoError(t, err)
	require.Equal(t, h1, h2)

	vs3 := NewValidatorSet(1, []Validator{v1, v2})
	h3, err := vs3.Hash()
	require.NoError(t, err)
	require.NotEqual(t, h1, h3)

}

func TestValidatorFromString(t *testing.T) {
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

	vs1 := NewValidatorSet(2, []Validator{v1, v2, v3})
	_, _, err = Membership(vs1.Validators)
	require.NoError(t, err)
}

func TestValidatorSetFromFile(t *testing.T) {
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

	vs1 := NewValidatorSet(0, []Validator{v1, v2, v3})
	require.Equal(t, 3, vs1.Size())
	require.Equal(t, uint64(0), vs1.GetConfigurationNumber())

	err = vs1.Save(fileName)
	require.NoError(t, err)

	vs2, err := NewValidatorSetFromFile(fileName)
	require.NoError(t, err)

	require.Equal(t, true, vs1.Equal(vs2))

	vs3 := NewValidatorSetFromValidators(0, []Validator{v1, v2, v3}...)
	require.Equal(t, true, vs1.Equal(vs3))
}

func TestValidatorSetFromString(t *testing.T) {
	s1 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	s2 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	s3 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"

	v1, err := NewValidatorFromString(s1)
	require.NoError(t, err)
	v2, err := NewValidatorFromString(s2)
	require.NoError(t, err)
	v3, err := NewValidatorFromString(s3)
	require.NoError(t, err)

	vs1 := NewValidatorSet(3, []Validator{v1, v2, v3})
	require.Equal(t, 3, vs1.Size())

	vs2, err := NewValidatorSetFromString("3;" + s1 + "," + s2 + "," + s3)
	require.NoError(t, err)

	require.Equal(t, true, vs1.Equal(vs2))
}

func TestValidatorSetFromJson(t *testing.T) {
	fileName := "_vs_test_file.tmp"
	t.Cleanup(func() {
		os.Remove(fileName) // nolint
	})

	s1 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10001/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"
	s2 := "t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10002/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ"

	v1, err := NewValidatorFromString(s1)
	require.NoError(t, err)
	v2, err := NewValidatorFromString(s2)
	require.NoError(t, err)

	vs2 := NewValidatorSet(2, []Validator{v1, v2})
	js := vs2.JSONString()
	fmt.Println(vs2.JSONString())

	var vs1 ValidatorSet
	err = json.Unmarshal([]byte(js), &vs1)
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

	vs1 := NewValidatorSet(0, []Validator{v1, v2, v3})
	require.Equal(t, 3, vs1.Size())

	vs2, err := NewValidatorSetFromFile(fileName)
	require.NoError(t, err)

	require.Equal(t, true, vs1.Equal(vs2))
}
