package mir

import (
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	u "github.com/ipfs/go-ipfs-util"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/lotus/chain"
)

/*  */
func TestCacheLen(t *testing.T) {
	mc := newDsBlkCache(datastore.NewMapDatastore(), chain.NewBadBlockCache())
	testCacheLen(t, mc)
	testCacheLen(t, mc)
}

func testCacheLen(t *testing.T, c *mirCache) {
	err := c.putBlk(10, cid.NewCidV0(u.Hash([]byte("req1"))))
	require.NoError(t, err)
	err = c.putBlk(11, cid.NewCidV0(u.Hash([]byte("req2"))))
	require.NoError(t, err)
	require.Equal(t, 2, c.length())
	err = c.rmBlk(11)
	require.NoError(t, err)
	require.Equal(t, 1, c.length())
	err = c.purge()
	require.NoError(t, err)
	require.Equal(t, 0, c.length())
}
