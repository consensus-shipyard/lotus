package mir

import (
	"testing"

	"github.com/ipfs/go-cid"
	u "github.com/ipfs/go-ipfs-util"
	"github.com/stretchr/testify/require"
)

func TestCheckpointInLotus(t *testing.T) {
	ch := CheckpointData{
		Checkpoint: Checkpoint{Height: 10, Parent: ParentMeta{Cid: cid.NewCidV0(u.Hash([]byte("req1"))), Height: 1}},
		Sn:         10,
		Config: EpochConfig{
			EpochNr:     11,
			Cert:        map[string]CertSig{"a": {[]byte("a")}, "b": {[]byte("b")}},
			Memberships: []Membership{{M: map[string]MembershipInfo{"a": {"a"}}}},
		},
	}

	vrfCheckpoint, err := ch.AsVRFProof()
	require.NoError(t, err)
	eproofCheckpoint, err := ch.ConfigAsElectionProof()
	require.NoError(t, err)
	// original checkpoint shouldn't have been modified.
	require.NotNil(t, ch.Config.Cert)
	require.NotNil(t, ch.Config.Memberships)

	out, err := CheckpointFromVRFProof(vrfCheckpoint)
	require.NoError(t, err)
	require.Equal(t, out.Checkpoint, ch.Checkpoint)
	cfg, err := ConfigFromElectionProof(eproofCheckpoint)
	require.NoError(t, err)
	require.Equal(t, ch.Config.Cert, cfg.Cert)

}
