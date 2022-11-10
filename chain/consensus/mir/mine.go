package mir

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p-core/host"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/mir"
	mirproto "github.com/filecoin-project/mir/pkg/pb/requestpb"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/types"
	ltypes "github.com/filecoin-project/lotus/chain/types"
)

const (
	ReconfigurationInterval = 2000 * time.Millisecond
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
func Mine(ctx context.Context, addr address.Address, h host.Host, api v1api.FullNode, db db.DB, cfg *Config) error {
	log.With("addr", addr).Infof("Mir miner started")
	defer log.With("addr", addr).Infof("Mir miner completed")

	m, err := NewManager(ctx, addr, h, api, db, cfg)
	if err != nil {
		return fmt.Errorf("unable to create a manager: %w", err)
	}

	// Perform cleanup of Node's modules and
	// ensure that mir is closed when we stop mining.
	// FIXME: I don't think this is working and stopping
	// the Mir transport correctly.
	defer m.Stop()

	log.Infof("Miner info:\n\twallet - %s\n\tsubnet - %s\n\tMir ID - %s\n\tPeer ID - %s\n\tvalidators - %v",
		m.Addr, m.NetName, m.MirID, h.ID(), m.InitialValidatorSet.GetValidators())

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
			log.Debug("Mir miner: context closed")
			return nil
		}
		base, err := api.ChainHead(ctx)
		if err != nil {
			log.Errorw("failed to get chain head", "error", err)
			continue
		}
		log.Debugf("Trying to mine new block over base: %s", base.Key())

		nextHeight := base.Height() + 1

		select {

		// first catch potential errors when mining
		case err := <-mirErrors:
			// return fmt.Errorf("miner consensus error: %w", err)
			//
			// TODO: This is a temporary solution while we are discussing that issue
			// https://filecoinproject.slack.com/archives/C03C77HN3AS/p1660330971306019
			if err != nil && !errors.Is(err, mir.ErrStopped) {
				panic(fmt.Errorf("miner consensus error: %w", err))
			}
			log.With("addr", addr).Infof("Mir node stopped signal")
			return nil

			// then jump to main loop
		default:
			// if not synced we are not ready to mine, this could lead to
			// races, so we need to wait.
			if !m.StateManager.isSynced() {
				log.Warnf("Mir not synced yet, waiting to sync before we start mining.")
				// add some delay for the case that next batch comes right after returning
				// this.
				time.Sleep(500 * time.Millisecond)
				continue
			}

			select {
			case membership := <-m.StateManager.NewMembership:
				if err := m.ReconfigureMirNode(ctx, membership); err != nil {
					log.With("epoch", nextHeight).Errorw("reconfiguring Mir failed", "error", err)
					continue
				}

			case <-reconfigure.C:
				// Send a reconfiguration transaction if the validator set in the actor has been changed.
				newValidatorSet, err := GetValidators(cfg.MembershipCfg)
				if err != nil {
					log.With("epoch", nextHeight).Warnf("failed to get subnet validators: %v", err)
					continue
				}

				if lastValidatorSet.Equal(newValidatorSet) {
					continue
				}

				log.With("epoch", nextHeight).Info("new validator set - size: %d", newValidatorSet.Size())
				lastValidatorSet = newValidatorSet

				if req := m.ReconfigurationRequest(newValidatorSet); req != nil {
					configRequests = append(configRequests, req)
				}

			case batch := <-m.StateManager.NextBatch:
				log.Debugf("Getting new batch from Mir to assemble a new block for height: %d", nextHeight)
				msgs := m.GetMessages(batch)
				log.With("epoch", nextHeight).
					Infof("try to create a block: msgs - %d", len(msgs))

				// include checkpoint in VRF proof field?
				vrfCheckpoint := &ltypes.Ticket{VRFProof: nil}
				eproofCheckpoint := &ltypes.ElectionProof{}
				if ch := m.StateManager.pollCheckpoint(); ch != nil {
					vrfCheckpoint, err = ch.AsVRFProof()
					if err != nil {
						log.With("epoch", nextHeight).Errorw("error getting vrfproof from checkpoint:", "error", err)
						continue
					}
					eproofCheckpoint, err = ch.ConfigAsElectionProof()
					if err != nil {
						log.With("epoch", nextHeight).Errorw("error getting eproof from checkpoint:", "error", err)
						continue
					}
					log.Infof("Including Mir checkpoint for height %d in block %d", ch.Checkpoint.Height, nextHeight)
				}

				bh, err := api.MinerCreateBlock(ctx, &lapi.BlockTemplate{
					// mir blocks are created by all miners. We use system actor as miner of the block
					Miner:            builtin.SystemActorAddr,
					Parents:          base.Key(),
					BeaconValues:     nil,
					Ticket:           vrfCheckpoint,
					Eproof:           eproofCheckpoint,
					Epoch:            base.Height() + 1,
					Timestamp:        uint64(time.Now().Unix()),
					WinningPoStProof: nil,
					Messages:         msgs,
				})
				if err != nil {
					log.With("epoch", nextHeight).Errorw("creating a block failed", "error", err)
					continue
				}
				if bh == nil {
					log.With("epoch", nextHeight).Debug("created a nil block")
					continue
				}

				err = api.SyncSubmitBlock(ctx, &types.BlockMsg{
					Header:        bh.Header,
					BlsMessages:   bh.BlsMessages,
					SecpkMessages: bh.SecpkMessages,
				})
				if err != nil {
					log.With("epoch", nextHeight).Errorw("unable to sync a block", "error", err)
					continue
				}

				log.With("epoch", nextHeight).Infof("mined a block at %d", bh.Header.Height)

			case toMir := <-m.ToMir:
				log.Debugf("selecting messages from mempool from base: %v", base.Key())
				msgs, err := api.MpoolSelect(ctx, base.Key(), 1)
				if err != nil {
					log.With("epoch", nextHeight).
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
}
