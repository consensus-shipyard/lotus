package db

import (
	"context"

	ds "github.com/ipfs/go-datastore"
)

type DB interface {
	Get(ctx context.Context, key ds.Key) (value []byte, err error)
	Put(ctx context.Context, key ds.Key, value []byte) error
	Delete(ctx context.Context, key ds.Key) error
}
