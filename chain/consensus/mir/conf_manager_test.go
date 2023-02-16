package mir

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	mirkv "github.com/filecoin-project/lotus/chain/consensus/mir/db/kv"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/types"
)

func TestRestoreConfigurationVotes(t *testing.T) {
	vs1 := []types.NodeID{types.NodeID("id1"), types.NodeID("id2")}
	vs2 := []types.NodeID{types.NodeID("id1")}
	votes := []VoteRecord{
		{0, "hash", NewVotedValidators(vs1...)},
		{34, "hash", NewVotedValidators(vs2...)},
	}

	m := GetConfigurationVotes(votes)
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

func TestConfigurationManagerMarshalConfigRequests(t *testing.T) {
	r0 := requestpb.Request{
		ReqNo:    1,
		ClientId: "1",
		Data:     []byte{0},
		Type:     ConfigurationRequest,
	}

	b, err := proto.Marshal(&r0)
	require.NoError(t, err)

	var r1 requestpb.Request
	err = proto.Unmarshal(b, &r1)
	require.NoError(t, err)
}

func TestConfigurationManagerDBOperations(t *testing.T) {
	dbFile := "cm_op_test.db"
	t.Cleanup(func() {
		err := os.RemoveAll(dbFile)
		require.NoError(t, err)
	})
	ds, err := mirkv.NewLevelDB(dbFile, false)
	require.NoError(t, err)
	cm := NewConfigurationManager(context.Background(), ds, "id1")

	cm.StoreSentConfigurationNumber(100)
	n := cm.GetSentConfigurationNumber()
	require.Equal(t, uint64(100), n)

	cm.StoreAppliedConfigurationNumber(200)
	n, found := cm.GetAppliedConfigurationNumber()
	require.True(t, found)
	require.Equal(t, uint64(200), n)

	r := requestpb.Request{
		ReqNo:    uint64(1),
		ClientId: "1",
		Data:     []byte{1},
		Type:     ConfigurationRequest,
	}

	err = cm.StoreConfigurationRequest(&r, 200)
	require.NoError(t, err)
	r1, err := cm.GetConfigurationRequest(200)
	require.NoError(t, err)
	require.EqualValues(t, r.ReqNo, r1.ReqNo)
}

// TestConfigurationManagerRecoverData tests that if we store two configuration requests then we can get them back.
func TestConfigurationManagerRecoverData(t *testing.T) {
	dbFile := "cm_recover_test.db"
	t.Cleanup(func() {
		err := os.RemoveAll(dbFile)
		require.NoError(t, err)
	})
	ds, err := mirkv.NewLevelDB(dbFile, false)
	require.NoError(t, err)
	cm := NewConfigurationManager(context.Background(), ds, "id1")

	nonce := uint64(0)

	// Store the first request.

	r0 := requestpb.Request{
		ReqNo:    nonce,
		ClientId: "1",
		Data:     []byte{0},
		Type:     ConfigurationRequest,
	}

	err = cm.StoreConfigurationRequest(&r0, r0.ReqNo)
	require.NoError(t, err)
	nonce++
	cm.StoreSentConfigurationNumber(r0.ReqNo)

	// Store the second request.
	r1 := requestpb.Request{
		ReqNo:    nonce,
		ClientId: "1",
		Data:     []byte{1},
		Type:     ConfigurationRequest,
	}

	err = cm.StoreConfigurationRequest(&r1, r1.ReqNo)
	require.NoError(t, err)
	nonce++
	cm.StoreSentConfigurationNumber(r1.ReqNo)

	// Recover the state and check it is correct.

	reqs, nn, err := cm.GetConfigurationData()
	require.NoError(t, err)
	require.Equal(t, uint64(1), nn)
	require.Equal(t, 2, len(reqs))

	// Execute the first request.

	cm.StoreAppliedConfigurationNumber(0)
	cm.RemoveConfigurationRequest(0)

	// Recover the state and check it is correct.
	reqs, nn, err = cm.GetConfigurationData()
	require.NoError(t, err)
	require.Equal(t, uint64(1), nn)
	require.Equal(t, 1, len(reqs))
	n, found := cm.GetAppliedConfigurationNumber()
	require.True(t, found)
	require.Equal(t, uint64(0), n)
}
