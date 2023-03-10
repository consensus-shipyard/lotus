package mir

import (
	"context"
	"fmt"

	"github.com/filecoin-project/mir/pkg/net"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
)

func Mine(ctx context.Context,
	transport net.Transport,
	node v1api.FullNode,
	db db.DB,
	membership membership.Reader,
	cfg *Config,
) error {
	m, err := NewManager(ctx, transport, node, db, membership, cfg)
	if err != nil {
		return fmt.Errorf("%v failed to create manager: %w", cfg.ID, err)
	}
	return m.Serve(ctx)
}
