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

type ErrMirCtxCanceledWhileWaitingBlock struct {
	Addr address.Address
}

func (e ErrMirCtxCanceledWhileWaitingBlock) Error() string {
	return fmt.Sprintf("validator %s context canceled while waiting for a snapshot", e.Addr)
}

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
	membership validator.Reader, cfg *Config) error {
	log.With("validator", addr).Infof("Mir validator started")
	defer log.With("validator", addr).Infof("Mir validator completed")

	m, err := NewManager(ctx, addr, h, api, db, membership, cfg)
	if err != nil {
		return fmt.Errorf("validator %v failed to create a manager: %w", addr, err)
	}

	// Perform cleanup of Node's modules and ensure that mir is closed when we stop mining.
	defer func() {
		m.Stop()
	}()

	log.Infof("validator info:\n\tNetwork - %v\n\tValidator ID - %v\n\tMir ID - %v\n\tMir peerID - %v\n\tMir peerID - %v",
		m.NetName, m.ValidatorID, m.MirID, h.ID(), m.InitialValidatorSet.GetValidators())

	mirErrors := m.Start(ctx)

	reconfigure := time.NewTicker(ReconfigurationInterval)
	defer reconfigure.Stop()

	lastValidatorSet := m.InitialValidatorSet

	var configRequest *mirproto.Request

	for {
		// Here we use `ctx.Err()` in the beginning of the `for` loop instead of using it in the `select` statement,
		// because if `ctx` has been closed then `api.ChainHead(ctx)` returns an error,
		// and we will be in the infinite loop due to `continue`.
		if ctx.Err() != nil {
			log.With("validator", addr).Debug("Mir validator: context closed")
			return nil
		}

		select {

		// first catch potential errors when mining
		case err := <-mirErrors:
			log.With("validator", addr).Info("validator received error signal:", err)
			if err != nil && !errors.Is(err, mir.ErrStopped) {
				panic(fmt.Sprintf("validator %s consensus error: %v", addr, err))
			}
			log.With("validator", addr).Infof("Mir node stopped signal")
			return nil

		case <-ctx.Done():
			log.With("validator", addr).Debug("Mir validator: context closed")
			return nil

		case <-reconfigure.C:
			// Send a reconfiguration transaction if the validator set in the actor has been changed.
			newSet, err := membership.GetValidatorSet()
			if err != nil {
				log.With("validator", addr).Warnf("failed to get subnet validators: %w", err)
				continue
			}

			if lastValidatorSet.Equal(newSet) {
				continue
			}

			log.With("validator", addr).
				Infof("new validator set: number: %d, size: %d, members: %v",
					newSet.ConfigurationNumber, newSet.Size(), newSet.GetValidatorIDs())

			lastValidatorSet = newSet
			configRequest = m.CreateReconfigurationRequest(newSet)

		case toMir := <-m.ToMir:
			base, err := m.StateManager.api.ChainHead(ctx)
			if err != nil {
				return xerrors.Errorf("validator %v failed to get chain head: %w", addr, err)
			}
			log.With("validator", addr).Debugf("selecting messages from mempool from base: %v", base.Key())
			msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
			if err != nil {
				log.With("validator", addr).With("epoch", base.Height()).
					Errorw("failed to select messages from mempool", "error", err)
			}

			requests := m.CreateTransportRequests(msgs)

			if configRequest != nil {
				requests = append(requests, configRequest)
				configRequest = nil
			}

			// We send requests via the channel instead of calling m.SubmitRequests(ctx, requests) explicitly.
			toMir <- requests
		}
	}
}
