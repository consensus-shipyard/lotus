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

const (
	MirCachePrefix = "mir-cache/"
	BlkCachePrefix = MirCachePrefix + "blk/"
)

var (
	latestCheckKey = datastore.NewKey(MirCachePrefix + "latestCheck")
)

func cacheKey(e abi.ChainEpoch) datastore.Key {
	return datastore.NewKey(BlkCachePrefix + strconv.FormatUint(uint64(e), 10))
}

type mirCache struct {
	cache blkCache
}

// blkCache used to track Mir unverified blocks,
// and verify them in bulk when a checkpoint is received.
type blkCache interface {
	get(e abi.ChainEpoch) (cid.Cid, error)
	put(e abi.ChainEpoch, v cid.Cid) error
	rm(e abi.ChainEpoch) error
	length() int
	setLatestCheckpoint(ch *CheckpointData) error
	getLatestCheckpoint() (*CheckpointData, error)
}

type dsBlkCache struct {
	ds datastore.Batching
}

func newDsBlkCache(ds datastore.Batching) *mirCache {
	return &mirCache{&dsBlkCache{ds: ds}}
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

func (c *dsBlkCache) setLatestCheckpoint(ch *CheckpointData) error {
	b, err := ch.Bytes()
	if err != nil {
		return fmt.Errorf("error serializing checkpoint data: %w", err)
	}
	return c.ds.Put(context.Background(), latestCheckKey, b)
}

func (c *dsBlkCache) getLatestCheckpoint() (*CheckpointData, error) {
	b, err := c.ds.Get(context.Background(), latestCheckKey)
	if err != nil {
		if err == datastore.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting latest checkpoint from Mir cache: %w", err)
	}
	ch := &CheckpointData{}
	err = ch.FromBytes(b)
	return ch, err
}

func (c mirCache) rcvCheckpoint(ch *CheckpointData) error {
	i := ch.Checkpoint.Height
	for _, k := range ch.Checkpoint.BlockCids {
		i--
		// bypass genesis
		if i == 0 {
			continue
		}
		log.Debugf("Getting block from mir cache for epoch: %d", i)
		v, err := c.cache.get(i)
		if err != nil {
			return fmt.Errorf("error getting value from datastore: %w", err)
		}
		if v == k {
			// delete from cache if verified by checkpoint
			if err := c.cache.rm(i); err != nil {
				return fmt.Errorf("error deleting value from datastore: %w", err)
			}
		} else {
			return fmt.Errorf("block verified in checkpoint not found in cache for epoch %d: %s v.s. %s", i, v, k)
		}
	}

	l := c.cache.length()
	// checkpoint doesn't verify all previous blocks.
	if l != 0 && l != -1 {
		return fmt.Errorf("checkpoint in block doesn't verify all previous unverified blocks in cache")
	}

	// update the latest checkpoint received.
	if err := c.cache.setLatestCheckpoint(ch); err != nil {
		if err != nil {
			return fmt.Errorf("couldn't persist latest checkpoint in cache: %w", err)
		}
	}

	return nil
}

func (c *mirCache) rcvBlock(b *types.BlockHeader) error {
	return c.cache.put(b.Height, b.Cid())
}

func (c *mirCache) latestCheckpoint() (*CheckpointData, error) {
	ch, err := c.cache.getLatestCheckpoint()
	if err != nil {
		return nil, err
	}
	if ch == nil {
		// if not found return empty CheckpointData
		return &CheckpointData{}, nil
	}
	return ch, nil
}

// thread-safe memory blockchain. It has low-overhead
// but it is not persisted between restarts (which may lead
// to inconsistencies).
//
// Learners may use this memory cache, but validators should
// persist their cache between restarts if they don't want to
// crash and get out of sync.
type memBlkCache struct {
	lk               sync.RWMutex
	m                map[abi.ChainEpoch]cid.Cid
	latestCheckpoint *CheckpointData
}

func newMemBlkCache() *mirCache {
	return &mirCache{&memBlkCache{m: make(map[abi.ChainEpoch]cid.Cid)}}
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
func (c *memBlkCache) setLatestCheckpoint(ch *CheckpointData) error {
	c.lk.Lock()
	defer c.lk.Unlock()
	c.latestCheckpoint = ch
	return nil

}

func (c *memBlkCache) getLatestCheckpoint() (*CheckpointData, error) {
	c.lk.RLock()
	defer c.lk.RUnlock()
	return c.latestCheckpoint, nil
}

var (
	_ blkCache = &memBlkCache{}
	_ blkCache = &dsBlkCache{}
)