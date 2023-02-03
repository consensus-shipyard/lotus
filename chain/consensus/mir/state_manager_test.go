package mir

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/mir/pkg/types"
)

func TestRestoreConfigurationVotes(t *testing.T) {
	vs1 := []types.NodeID{types.NodeID("id1"), types.NodeID("id2")}
	vs2 := []types.NodeID{types.NodeID("id1")}
	votes := []VoteRecord{
		{0, "hash", NewVotedValidators(vs1...)},
		{34, "hash", NewVotedValidators(vs2...)},
	}

	m := RestoreConfigurationVotes(votes)
	require.Equal(t, 2, len(m))
	require.Equal(t, 2, len(m[0]["hash"]))
	require.Equal(t, 1, len(m[34]["hash"]))
}

func TestStoreConfigurationVotes(t *testing.T) {
	m := make(map[uint64]map[string][]types.NodeID)
	m[0] = make(map[string][]types.NodeID)
	m[0]["aa"] = []types.NodeID{"id1", "id2"}
	m[0]["bb"] = []types.NodeID{"id1"}

	votes := StoreConfigurationVotes(m)
	require.Equal(t, 2, len(votes))
	require.Equal(t, uint64(0), votes[0].ConfigurationNumber, uint64(0))
	require.Equal(t, uint64(0), votes[1].ConfigurationNumber, uint64(0))
}

func TestConfigurationMarshalling(t *testing.T) {
	v := VoteRecord{
		ConfigurationNumber: 1,
		ValSetHash:          "hash",
		VotedValidators:     NewVotedValidators([]types.NodeID{"id1"}...),
	}

	buf := new(bytes.Buffer)
	err := v.MarshalCBOR(buf)
	require.NoError(t, err)

	err = v.UnmarshalCBOR(buf)
	require.NoError(t, err)
}
