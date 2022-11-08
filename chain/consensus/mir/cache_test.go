package mir

import (
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	u "github.com/ipfs/go-ipfs-util"
	"github.com/stretchr/testify/require"
)

func TestCacheLen(t *testing.T) {
	mc := newDsBlkCache(datastore.NewMapDatastore())
	dc := newMemBlkCache()
	testCacheLen(t, mc)
	testCacheLen(t, dc)

}

func testCacheLen(t *testing.T, c *mirCache) {
	err := c.cache.put(10, cid.NewCidV0(u.Hash([]byte("req1"))))
	require.NoError(t, err)
	err = c.cache.put(11, cid.NewCidV0(u.Hash([]byte("req2"))))
	require.NoError(t, err)
	require.Equal(t, 2, c.cache.length())
	err = c.cache.rm(11)
	require.NoError(t, err)
	require.Equal(t, 1, c.cache.length())
	err = c.cache.purge()
	require.NoError(t, err)
	require.Equal(t, 0, c.cache.length())
}
