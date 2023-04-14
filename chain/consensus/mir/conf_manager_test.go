package mir

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	"github.com/filecoin-project/mir/pkg/types"

	mirkv "github.com/filecoin-project/lotus/chain/consensus/mir/db/kv"
)

func TestRestoreConfigurationVotes(t *testing.T) {
	valSet1 := []types.NodeID{types.NodeID("id1"), types.NodeID("id2")}
	valSet2 := []types.NodeID{types.NodeID("id1")}
	votes := []VoteRecord{
		{0, "hash1", NewVotedValidators(valSet1...)},
		{34, "hash2", NewVotedValidators(valSet2...)},
	}

	m := GetConfigurationVotes(votes)
	require.Equal(t, 2, len(valSet1))
	require.Equal(t, 1, len(valSet2))
	require.Equal(t, 2, len(m))
	require.Equal(t, 2, len(m[0]["hash1"]))
	require.Equal(t, struct{}{}, m[0]["hash1"]["id1"])
	require.Equal(t, struct{}{}, m[0]["hash1"]["id2"])

	require.Equal(t, 1, len(m[34]["hash2"]))
	require.Equal(t, struct{}{}, m[0]["hash2"]["id1"])
}

func TestStoreConfigurationVotes(t *testing.T) {
	m := make(map[uint64]map[string]map[types.NodeID]struct{})
	m[0] = make(map[string]map[types.NodeID]struct{})
	m[0]["aa"] = make(map[types.NodeID]struct{})
	m[0]["aa"]["id1"] = struct{}{}
	m[0]["aa"]["id2"] = struct{}{}
	m[0]["bb"] = make(map[types.NodeID]struct{})
	m[0]["bb"]["id1"] = struct{}{}

	voteRecords := StoreConfigurationVotes(m)
	require.Equal(t, 2, len(voteRecords))
	require.Equal(t, uint64(0), voteRecords[0].ConfigurationNumber, uint64(0))
	require.Equal(t, uint64(0), voteRecords[1].ConfigurationNumber, uint64(0))
}

func TestMarshalling(t *testing.T) {
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

	// ----

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
	cm, err := NewConfigurationManager(context.Background(), ds, "id1")
	require.NoError(t, err)
	require.Equal(t, uint64(0), cm.nextReqNo)
	require.Equal(t, uint64(0), cm.nextAppliedNo)

	cm.storeNextConfigurationNumber(100)
	n := cm.getNextConfigurationNumber()
	require.Equal(t, uint64(100), n)

	cm.storeNextAppliedConfigurationNumber(200)
	n = cm.getAppliedConfigurationNumber()
	require.Equal(t, uint64(200), n)

	r := requestpb.Request{
		ReqNo:    uint64(1),
		ClientId: "1",
		Data:     []byte{1},
		Type:     ConfigurationRequest,
	}

	err = cm.storeRequest(&r, 200)
	require.NoError(t, err)
	r1, err := cm.getRequest(200)
	require.NoError(t, err)
	require.EqualValues(t, r.ReqNo, r1.ReqNo)
}

// TestConfigurationManagerRecoverData_NoCrash tests that if we store two configuration requests then we can get them back.
func TestConfigurationManagerRecoverData_NoCrash(t *testing.T) {
	dbFile := "cm_recover_test_nocrash.db"
	t.Cleanup(func() {
		err := os.RemoveAll(dbFile)
		require.NoError(t, err)
	})
	ds, err := mirkv.NewLevelDB(dbFile, false)
	require.NoError(t, err)
	cm, err := NewConfigurationManager(context.Background(), ds, "id1")
	require.NoError(t, err)

	_, err = cm.NewTX(ConfigurationRequest, []byte{0})
	require.NoError(t, err)

	_, err = cm.NewTX(ConfigurationRequest, []byte{1})
	require.NoError(t, err)

	// Recover the state and check it is correct.

	reqs, err := cm.Pending()
	require.NoError(t, err)
	require.Equal(t, 2, len(reqs))
	require.True(t, bytes.Equal(reqs[0].Data, []byte{0}))
	require.True(t, bytes.Equal(reqs[1].Data, []byte{1}))

	err = cm.Done(types.ReqNo(reqs[0].ReqNo))
	require.NoError(t, err)
	reqs, err = cm.Pending()
	require.NoError(t, err)
	require.Equal(t, 1, len(reqs))
	require.True(t, bytes.Equal(reqs[0].Data, []byte{1}))

	// Check the configuration internal state
	require.Equal(t, uint64(2), cm.nextReqNo)
	require.Equal(t, uint64(1), cm.nextAppliedNo)
}

// TestConfigurationManagerNewTX_Atomicity tests that NewTX can be expressed via
// StoreConfigurationRequest and StoreNextConfigurationNumber.
func TestConfigurationManagerNewTX_Atomicity(t *testing.T) {
	dbFile1 := "cm_recover_test_exp1.db"
	dbFile2 := "cm_recover_test_exp2.db"
	t.Cleanup(func() {
		err := os.RemoveAll(dbFile1)
		require.NoError(t, err)

		err = os.RemoveAll(dbFile2)
		require.NoError(t, err)
	})

	ds1, err := mirkv.NewLevelDB(dbFile1, false)
	require.NoError(t, err)
	cm1, err := NewConfigurationManager(context.Background(), ds1, "id1")
	require.NoError(t, err)

	_, err = cm1.NewTX(ConfigurationRequest, []byte{0})
	require.NoError(t, err)

	_, err = cm1.NewTX(ConfigurationRequest, []byte{1})
	require.NoError(t, err)

	// ---

	ds2, err := mirkv.NewLevelDB(dbFile2, false)
	require.NoError(t, err)
	cm2, err := NewConfigurationManager(context.Background(), ds2, "id2")
	require.NoError(t, err)

	// Store the first request.

	r0 := requestpb.Request{
		ReqNo:    cm2.nextReqNo,
		ClientId: "1",
		Data:     []byte{0},
		Type:     ConfigurationRequest,
	}

	err = cm2.storeRequest(&r0, r0.ReqNo)
	require.NoError(t, err)
	cm2.nextReqNo++
	cm2.storeNextConfigurationNumber(cm2.nextReqNo)

	// Store the second request.
	r1 := requestpb.Request{
		ReqNo:    cm2.nextReqNo,
		ClientId: "1",
		Data:     []byte{1},
		Type:     ConfigurationRequest,
	}

	err = cm2.storeRequest(&r1, r1.ReqNo)
	require.NoError(t, err)
	cm2.nextReqNo++
	cm2.storeNextConfigurationNumber(cm2.nextReqNo)

	// check DBs store the same data

	require.Equal(t, cm2.getNextConfigurationNumber(), cm1.nextReqNo)
	require.Equal(t, cm2.getAppliedConfigurationNumber(), cm1.nextAppliedNo)

	reqs1, err := cm1.Pending()
	require.NoError(t, err)
	reqs2, err := cm2.Pending()
	require.NoError(t, err)
	require.Equal(t, len(reqs2), len(reqs1))
}

// TestConfigurationManagerRecoverData_WithCrash tests that if we store two configuration requests then we can get them
// back even if crash happened.
func TestConfigurationManagerRecoverData_WithCrash(t *testing.T) {
	dbFile := "cm_recover_test_withcrash.db"
	t.Cleanup(func() {
		err := os.RemoveAll(dbFile)
		require.NoError(t, err)
	})
	ds, err := mirkv.NewLevelDB(dbFile, false)
	require.NoError(t, err)
	cm, err := NewConfigurationManager(context.Background(), ds, "id1")
	require.NoError(t, err)

	// Store the first request.
	_, err = cm.NewTX(ConfigurationRequest, []byte{0})
	require.NoError(t, err)

	// Store the second request using low level primitive to simulate a crash.
	r1 := requestpb.Request{
		ReqNo:    1,
		ClientId: "1",
		Data:     []byte{1},
		Type:     ConfigurationRequest,
	}
	err = cm.storeRequest(&r1, r1.ReqNo)
	require.NoError(t, err)
	cm.storeNextConfigurationNumber(uint64(2))

	// Recover the state and check it is correct.

	cm, err = NewConfigurationManager(context.Background(), ds, "id1")
	require.NoError(t, err)

	reqs, err := cm.Pending()
	require.NoError(t, err)
	require.Equal(t, uint64(2), cm.nextReqNo)
	require.Equal(t, uint64(0), cm.nextAppliedNo)
	require.Equal(t, 2, len(reqs))
	require.True(t, bytes.Equal(reqs[0].Data, []byte{0}))
	require.True(t, bytes.Equal(reqs[1].Data, []byte{1}))

	// Execute the first request.

	err = cm.Done(types.ReqNo(reqs[0].ReqNo))
	require.NoError(t, err)
	reqs, err = cm.Pending()
	require.NoError(t, err)
	require.Equal(t, 1, len(reqs))
	require.True(t, bytes.Equal(reqs[0].Data, []byte{1}))

	// Check the configuration internal state
	require.Equal(t, uint64(2), cm.nextReqNo)
	require.Equal(t, uint64(1), cm.nextAppliedNo)
}

// TestConfigurationManagerRecoverData_WithZeroNonce tests that if we crash on the first request.
func TestConfigurationManagerRecoverData_WithZeroNonce0(t *testing.T) {
	dbFile := "cm_recover_test_0.db"
	t.Cleanup(func() {
		err := os.RemoveAll(dbFile)
		require.NoError(t, err)
	})
	ds, err := mirkv.NewLevelDB(dbFile, false)
	require.NoError(t, err)
	cm, err := NewConfigurationManager(context.Background(), ds, "id1")
	require.NoError(t, err)

	// Store the first request.

	// Recover the state and check it is correct.

	reqs, err := cm.Pending()
	require.NoError(t, err)
	require.Equal(t, uint64(0), cm.nextReqNo)
	require.Equal(t, uint64(0), cm.nextAppliedNo)
	require.Equal(t, 0, len(reqs))
}
