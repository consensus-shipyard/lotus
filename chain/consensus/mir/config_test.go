package mir

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
)

func TestConfigBasic(t *testing.T) {
	addr, err := address.NewFromString("t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy")
	require.NoError(t, err)

	cfg, err := NewConfig(addr,
		"dbpath",
		nil,
		"repo",
		1,
		2,
		"1s",
		"http://127.0.0.1",
		membership.FileSource,
	)
	require.NoError(t, err)

	require.Equal(t, 2, cfg.Consensus.ConfigOffset)
	require.Equal(t, 1, cfg.Consensus.SegmentLength)
	require.Equal(t, 1*time.Second, cfg.Consensus.MaxProposeDelay)
	require.Equal(t, 6*time.Second, cfg.Consensus.PBFTViewChangeSegmentTimeout)
	require.Equal(t, 6*time.Second, cfg.Consensus.PBFTViewChangeSNTimeout)
	require.Equal(t, 1024, cfg.Consensus.MaxTransactionsInBatch)
	require.Equal(t, "file", cfg.MembershipSourceValue)
}
