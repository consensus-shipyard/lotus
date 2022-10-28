package mir

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/chain/types"
)

const BlkCachePrefix = "mir-cache/"

func cacheKey(e abi.ChainEpoch) datastore.Key {
	return datastore.NewKey(BlkCachePrefix + strconv.FormatUint(uint64(e), 10))
}

// blkCache used to track Mir unverified blocks,
// and verify them in bulk when a checkpoint is received.
type blkCache interface {
	rcvCheckpoint(ch *CheckpointData) error
	rcvBlock(b *types.BlockHeader) error
	// basic operations
	get(e abi.ChainEpoch) (cid.Cid, error)
	put(e abi.ChainEpoch, v cid.Cid) error
	rm(e abi.ChainEpoch) error
	length() int
}

//
type dsBlkCache struct {
	ds datastore.Batching
}

func newDsBlkCache(ds datastore.Batching) *dsBlkCache {
	return &dsBlkCache{ds: ds}
}

func (c *dsBlkCache) get(e abi.ChainEpoch) (cid.Cid, error) {
	v, err := c.ds.Get(context.Background(), cacheKey(e))
	if err != nil {
		return cid.Undef, err
	}
	_, cid, err := cid.CidFromBytes(v)
	return cid, err
}

func (c *dsBlkCache) put(e abi.ChainEpoch, v cid.Cid) error {
	return c.ds.Put(context.Background(), cacheKey(e), v.Bytes())
}

func (c *dsBlkCache) rm(e abi.ChainEpoch) error {
	return c.ds.Delete(context.Background(), cacheKey(e))
}

// this operation is potentially expensive according
// to the underlying datastore and the size of the
// datastore.
func (c *dsBlkCache) length() int {
	q := query.Query{Prefix: BlkCachePrefix}
	i := 0
	qr, _ := c.ds.Query(context.Background(), q)
	for r := range qr.Next() {
		if r.Error != nil {
			return -1
		}
		i++
	}
	return i
}

func (c *dsBlkCache) rcvCheckpoint(ch *CheckpointData) error {
	return rcvCheckpoint(c, ch)
}

func rcvCheckpoint(c blkCache, ch *CheckpointData) error {
	i := ch.Checkpoint.Height
	for _, k := range ch.Checkpoint.BlockCids {
		i--
		// bypass genesis
		if i == 0 {
			continue
		}
		fmt.Println("Getting block from cache for epoch", i)
		v, err := c.get(i)
		if err != nil {
			return fmt.Errorf("error getting value from datastore: %w", err)
		}
		if v == k {
			// delete from cache if verified by checkpoint
			if err := c.rm(i); err != nil {
				return fmt.Errorf("error deleting value from datastore: %w", err)
			}
		} else {
			return fmt.Errorf("block verified in checkpoint not found in cache for epoch %d: %s v.s. %s", i, v, k)
		}
	}

	l := c.length()
	// checkpoint doesn't verify all previous blocks.
	if l != 0 && l != -1 {
		return fmt.Errorf("checkpoint in block doesn't verify all previous unverified blocks in cache")
	}

	return nil
}

func (c *dsBlkCache) rcvBlock(b *types.BlockHeader) error {
	return rcvBlock(c, b)
}

func rcvBlock(c blkCache, b *types.BlockHeader) error {
	return c.put(b.Height, b.Cid())
}

// thread-safe memory blockchain. It has low-overhead
// but it is not persisted between restarts (which may lead
// to inconsistencies).
//
// Learners may use this memory cache, but validators should
// persist their cache between restarts if they don't want to
// crash and get out of sync.
type memBlkCache struct {
	lk sync.RWMutex
	m  map[abi.ChainEpoch]cid.Cid
}

func newMemBlkCache() *memBlkCache {
	return &memBlkCache{m: make(map[abi.ChainEpoch]cid.Cid)}
}

func (c *memBlkCache) get(e abi.ChainEpoch) (cid.Cid, error) {
	c.lk.RLock()
	defer c.lk.RUnlock()
	return c.m[e], nil
}

func (c *memBlkCache) put(e abi.ChainEpoch, v cid.Cid) error {
	c.lk.Lock()
	defer c.lk.Unlock()
	c.m[e] = v
	return nil
}

func (c *memBlkCache) rm(e abi.ChainEpoch) error {
	c.lk.Lock()
	defer c.lk.Unlock()
	delete(c.m, e)
	return nil
}

func (c *memBlkCache) length() int {
	c.lk.RLock()
	defer c.lk.RUnlock()
	return len(c.m)
}

func (c *memBlkCache) rcvCheckpoint(ch *CheckpointData) error {
	fmt.Println("===== CACHE:", c.m)
	return rcvCheckpoint(c, ch)
}

func (c *memBlkCache) rcvBlock(b *types.BlockHeader) error {
	return rcvBlock(c, b)
}

var (
	_ blkCache = &memBlkCache{}
	_ blkCache = &dsBlkCache{}
)
