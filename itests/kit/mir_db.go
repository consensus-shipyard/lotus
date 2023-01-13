package kit

import (
	"context"
	"sync"

	ds "github.com/ipfs/go-datastore"
)

type TestDB struct {
	db   map[ds.Key][]byte
	lock sync.Mutex
}

func NewTestDB() *TestDB {
	return &TestDB{
		db: make(map[ds.Key][]byte),
	}
}

func (kv *TestDB) Get(ctx context.Context, key ds.Key) (value []byte, err error) {
	kv.lock.Lock()
	defer kv.lock.Unlock()
	v, ok := kv.db[key]
	if !ok {
		return nil, ds.ErrNotFound
	}
	return v, nil
}

func (kv *TestDB) Put(ctx context.Context, key ds.Key, value []byte) error {
	kv.lock.Lock()
	defer kv.lock.Unlock()
	kv.db[key] = value
	return nil
}
