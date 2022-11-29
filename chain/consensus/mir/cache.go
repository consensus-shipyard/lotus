package mir

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	CachePrefix      = "mir-cache/"
	BlkCachePrefix   = CachePrefix + "blk/"
	CheckCachePrefix = CachePrefix + "check/"
)

var (
	latestCheckKey = datastore.NewKey(CachePrefix + "latestCheck")
)

func blkCacheKey(e abi.ChainEpoch) datastore.Key {
	return datastore.NewKey(BlkCachePrefix + strconv.FormatUint(uint64(e), 10))
}

func checkCacheKey(e abi.ChainEpoch) datastore.Key {
	return datastore.NewKey(CheckCachePrefix + strconv.FormatUint(uint64(e), 10))
}

func heightFromBlkKey(k string) (abi.ChainEpoch, error) {
	n, err := strconv.Atoi(strings.Split(k, BlkCachePrefix)[1])
	if err != nil {
		return -1, err
	}
	return abi.ChainEpoch(n), nil
}

type mirCache struct {
	ds datastore.Batching
	// lock to avoid starting bad block
	// marking processes in parallel
	badBlkLk sync.Mutex
	badBlk   *chain.BadBlockCache
}

func newDsBlkCache(ds datastore.Batching, bad *chain.BadBlockCache) *mirCache {
	return &mirCache{ds: ds, badBlk: bad}
}

func (c *mirCache) getBlk(e abi.ChainEpoch) (cid.Cid, error) {
	v, err := c.ds.Get(context.Background(), blkCacheKey(e))
	if err != nil {
		if err == datastore.ErrNotFound {
			return cid.Undef, nil
		}
		return cid.Undef, err
	}
	_, one, err := cid.CidFromBytes(v)
	return one, err
}

func (c *mirCache) putBlk(e abi.ChainEpoch, v cid.Cid) error {
	return c.ds.Put(context.Background(), blkCacheKey(e), v.Bytes())
}

func (c *mirCache) rmBlk(e abi.ChainEpoch) error {
	return c.ds.Delete(context.Background(), blkCacheKey(e))
}

func (c *mirCache) getCheck(e abi.ChainEpoch) (*Checkpoint, error) {
	v, err := c.ds.Get(context.Background(), checkCacheKey(e))
	if err != nil {
		if err == datastore.ErrNotFound {
			return &Checkpoint{}, nil
		}
		return nil, err
	}
	ch := &Checkpoint{}
	err = ch.FromBytes(v)
	if err != nil {
		return nil, err
	}
	return ch, nil
}

func (c *mirCache) putCheck(v *Checkpoint) error {
	b, err := v.Bytes()
	if err != nil {
		return err
	}
	return c.putCheckInBytes(v.Height, b)
}

func (c *mirCache) putCheckInBytes(height abi.ChainEpoch, b []byte) error {
	return c.ds.Put(context.Background(), checkCacheKey(height), b)
}

func (c *mirCache) rmCheck(e abi.ChainEpoch) error {
	return c.ds.Delete(context.Background(), checkCacheKey(e))
}

// TODO: For long checkpoint periods this operation may take a
// while, this is supposed to only be done when RestoreState is
// called and we are recovering from a checkpoint, but maybe we
// should consider performing this in the background to remove it
// from the critical path.
func (c *mirCache) purge() error {
	if err := c.purgeByPrefix(BlkCachePrefix); err != nil {
		return err
	}
	return c.purgeByPrefix(CheckCachePrefix)
}

func (c *mirCache) purgeByPrefix(prefix string) error {
	q := query.Query{Prefix: prefix}
	qr, _ := c.ds.Query(context.Background(), q)
	entries, err := qr.Rest()
	if err != nil {
		return fmt.Errorf("error performing cache query for purge prefix %s: %w", prefix, err)
	}
	for _, e := range entries {
		return c.ds.Delete(context.Background(), datastore.NewKey(e.Key))
	}
	return nil
}

// this operation is potentially expensive according
// to the underlying datastore and the size of the
// datastore.
func (c *mirCache) length() int {
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

func (c *mirCache) getLatestCheckpoint() (*Checkpoint, error) {
	b, err := c.ds.Get(context.Background(), latestCheckKey)
	if err != nil {
		if err == datastore.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting latest checkpoint from Mir cache: %w", err)
	}
	ch := &Checkpoint{}
	err = ch.FromBytes(b)
	return ch, err
}

func (c *mirCache) rcvCheckpoint(snap *Checkpoint) error {
	prev, err := c.prevCheckpoint(snap)
	if err != nil {
		return err
	}
	i := snap.Height
	for _, k := range snap.BlockCids {
		i--
		// bypass genesis
		if i == 0 {
			continue
		}
		log.Debugf("Getting block from mir cache for epoch: %d", i)
		v, err := c.getBlk(i)
		if err != nil {
			return fmt.Errorf("error getting value from datastore: %w", err)
		}
		if v == cid.Undef {
			// this usually happens when restarting a node, if the block is already on-chain
			// but we receive the following checkpoint that wasn't received yet.
			log.Warnf("missing unverified block for that height %d in cache. It may have been verified already", i)
			continue
		}
		if v == k {
			// delete from cache if verified by checkpoint
			if err := c.rmBlk(i); err != nil {
				return fmt.Errorf("error deleting value from datastore: %w", err)
			}
		} else {
			return fmt.Errorf("block verified in checkpoint not found in cache for epoch %d: %s v.s. %s", i, v, k)
		}
	}

	// verify that all block in range have been verified
	// TODO: Mark as bad those that have not been verified and purge
	// the block store?
	if i != prev.Height {
		log.Warnf("Checkpoint didn't verify the whole gap of blocks between checkpoints: %d, %d", i, prev.Height)
	}

	// update the latest checkpoint received.
	if err := c.setLatestCheckpoint(snap); err != nil {
		return fmt.Errorf("couldn't persist latest checkpoint in cache: %w", err)
	}

	// mark bad blocks
	// including it on a routine to take it out of the critical path.
	go c.markBadBlks(snap.Height)

	return nil
}

func (c *mirCache) rcvBlock(b *types.BlockHeader) error {
	if c, _ := c.getBlk(b.Height); c != cid.Undef {
		// if someone is trying to push a new rcvBlock
		if c != b.Cid() {
			return fmt.Errorf("already seen a block for that height in cache: height=%d", b.Height)
		}
		return nil
	}
	return c.putBlk(b.Height, b.Cid())
}

// return previous checkpoint for checkpoint at epoch e.
// returns empty checkpoint if no previous checkpoint.
func (c *mirCache) prevCheckpoint(snap *Checkpoint) (*Checkpoint, error) {
	return c.getCheck(snap.Parent.Height)
}

func (c *mirCache) setLatestCheckpoint(snap *Checkpoint) error {
	b, err := snap.Bytes()
	if err != nil {
		return fmt.Errorf("error serializing checkpoint data: %w", err)
	}
	if err := c.putCheckInBytes(snap.Height, b); err != nil {
		return err
	}
	if err := c.ds.Put(context.Background(), latestCheckKey, b); err != nil {
		return err
	}
	// garbage collect the previous checkpoint pointed by this one.
	// Potentially not needed anymore if rcvCheckpoint was called.
	return c.rmCheck(snap.Parent.Height)
}

// if a block with a height below a verify checkpoint hasn't been
// removed from the cache is because it is bad (or outdated) and it should be marked
// as such.
func (c *mirCache) markBadBlks(height abi.ChainEpoch) {
	// sequentialize badblks marking
	c.badBlkLk.Lock()
	defer c.badBlkLk.Unlock()

	q := query.Query{Prefix: BlkCachePrefix}
	qr, _ := c.ds.Query(context.Background(), q)
	for r := range qr.Next() {
		if r.Error != nil {
			log.Errorf("error marking bad blocks for height: %d: %w", height, r.Error)
			return
		}
		h, err := heightFromBlkKey(r.Key)
		if err != nil {
			log.Errorf("error getting key height: %w", err)
			continue
		}
		if h < height {
			// the cid for the badBlockReason should the cid for the tipset or block
			// where it is verified.
			_, vcid, err := cid.CidFromBytes(r.Value)
			if err != nil {
				log.Errorf("error getting cid for block from ds:  %w", err)
				continue

			}
			c.badBlk.Add(vcid, chain.NewBadBlockReason([]cid.Cid{vcid}, "block not verified by mir checkpoint"))
		}
	}
}
