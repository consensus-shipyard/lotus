package mir

import (
	"fmt"
	"sync"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/chain/types"
)

type blkCache struct {
	lk sync.Mutex
	m  map[abi.ChainEpoch]cid.Cid
}

func newMirBlkCache() *blkCache {
	return &blkCache{m: make(map[abi.ChainEpoch]cid.Cid)}
}

func (c *blkCache) rcvCheckpoint(ch *CheckpointData) error {
	c.lk.Lock()
	defer c.lk.Unlock()
	i := ch.Checkpoint.Height
	fmt.Println("xxx===================", c.m)
	for _, k := range ch.Checkpoint.BlockCids {
		i--
		// bypass genesis
		if i == 0 {
			continue
		}
		if c.m[i] == k {
			// delete from cache if verified by checkpoint
			delete(c.m, i)
		} else {
			return fmt.Errorf("block verified in checkpoint not found in cache for epoch %d: %s v.s. %s", i, c.m[i], k)
		}
	}
	// checkpoint doesn't verify all previous blocks.
	if len(c.m) != 0 {
		return fmt.Errorf("checkpoint in block doesn't verify all previous unverified blocks in cache")
	}

	return nil
}

func (c *blkCache) rcvBlock(b *types.BlockHeader) {
	c.lk.Lock()
	defer c.lk.Unlock()
	fmt.Println("======= RECEIVED BLOCK FOR EPOCH AND CID", b.Height, b.Cid())
	c.m[b.Height] = b.Cid()
}
