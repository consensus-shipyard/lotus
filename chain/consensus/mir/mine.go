package mir

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p-core/host"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/validator"
)

func Mine(ctx context.Context,
	addr address.Address,
	h host.Host,
	api v1api.FullNode,
	db db.DB,
	membership validator.Reader,
	cfg *Config,
) error {
	m, err := NewManager(ctx, addr, h, api, db, membership, cfg)
	if err != nil {
		return fmt.Errorf("%v failed to create manager: %w", addr, err)
	}
	return m.Serve(ctx)
}
