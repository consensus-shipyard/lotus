package mir

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/validator"
	"github.com/filecoin-project/mir"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"
)

const (
	ReconfigurationInterval = 2000 * time.Millisecond
)

var (
	ErrMirCtxCanceledWhileWaitingSnapshot = fmt.Errorf("context canceled while wating for a snapshot")
)

// Mine implements "block mining" using the Mir framework.
//
// Mine implements the following main algorithm:
//  1. Retrieve messages and cross-messages from the mempool.
//     Note that messages can be added into mempool via the libp2p and CLI mechanism.
//  2. Send messages and cross messages to the Mir node through the request pool implementing FIFO.
//  3. Receive ordered messages from the Mir node and parse them.
//  4. Create the next Filecoin block.
//  5. Broadcast this block to the rest of the network. Validators will not accept broadcasted,
//     they already have it.
//  6. Sync and restore from state whenever needed.
func Mine(ctx context.Context, addr address.Address, h host.Host, api v1api.FullNode, db db.DB,
	membership validator.MembershipReader, cfg *Config) error {
	log.With("miner", addr).Infof("Mir miner started")
	defer log.With("miner", addr).Infof("Mir miner completed")

	m, err := NewManager(ctx, addr, h, api, db, membership, cfg)
	if err != nil {
		return fmt.Errorf("unable to create a manager: %w", err)
	}

	// Perform cleanup of Node's modules and ensure that mir is closed when we stop mining.
	defer func() {
		m.Stop()
	}()

	log.Infof("Miner info:\n\tNetwork - %v\n\tValidator ID - %v\n\tMir ID - %v\n\tMir peerID - %v\n\tMir peerID - %v",
		m.NetName, m.ValidatorID, m.MirID, h.ID(), m.InitialValidatorSet.GetValidators())

	mirErrors := m.Start(ctx)

	reconfigure := time.NewTicker(ReconfigurationInterval)
	defer reconfigure.Stop()

	lastValidatorSet := m.InitialValidatorSet

	var configRequests []*mirproto.Request

	for {
		// Here we use `ctx.Err()` in the beginning of the `for` loop instead of using it in the `select` statement,
		// because if `ctx` has been closed then `api.ChainHead(ctx)` returns an error,
		// and we will be in the infinite loop due to `continue`.
		if ctx.Err() != nil {
			log.With("validator", addr).Debug("Mir miner: context closed")
			return nil
		}

		select {

		// first catch potential errors when mining
		case err := <-mirErrors:
			log.With("miner", addr).Info("Miner received error signal:", err)
			if err != nil && !errors.Is(err, mir.ErrStopped) {
				panic(fmt.Sprintf("miner %s consensus error: %v", addr, err))
			}
			log.With("miner", addr).Infof("Mir node stopped signal")
			return nil

		case <-ctx.Done():
			log.With("miner", addr).Debug("Mir miner: context closed")
			return nil

		case <-reconfigure.C:
			// Send a reconfiguration transaction if the validator set in the actor has been changed.
			newValidatorSet, err := membership.GetValidators()
			if err != nil {
				log.With("miner", addr).Warnf("failed to get subnet validators: %w", err)
				continue
			}

			if lastValidatorSet.Equal(newValidatorSet) {
				continue
			}

			log.With("miner", addr).Infof("new validator set - size: %d", newValidatorSet.Size())
			lastValidatorSet = newValidatorSet

			if req := m.ReconfigurationRequest(newValidatorSet); req != nil {
				configRequests = append(configRequests, req)
			}

		case toMir := <-m.ToMir:
			base, err := m.StateManager.api.ChainHead(ctx)
			if err != nil {
				return xerrors.Errorf("failed to get chain head: %w", err)
			}
			log.With("miner", addr).Debugf("selecting messages from mempool from base: %v", base.Key())
			msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.With("epoch", base.Height()).
					Errorw("unable to select messages from mempool", "error", err)
			}

			requests := m.TransportRequests(msgs)

			if len(configRequests) > 0 {
				requests = append(requests, configRequests...)
				configRequests = nil
			}

			// We send requests via the channel instead of calling m.SubmitRequests(ctx, requests) explicitly.
			toMir <- requests
		}
	}
}
