package genesis

import (
	"context"

	ipctypes "github.com/consensus-shipyard/go-ipc-types/sdk"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/manifest"

	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	"github.com/filecoin-project/lotus/chain/types"
)

func SetupRewardActor(ctx context.Context, bs bstore.Blockstore, qaPower big.Int, av actorstypes.Version, networkName string) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	rst, err := reward.MakeState(adt.WrapStore(ctx, cst), av, qaPower)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, rst.GetState())
	if err != nil {
		return nil, err
	}

	actcid, ok := actors.GetActorCodeID(av, manifest.RewardKey)
	if !ok {
		return nil, xerrors.Errorf("failed to get reward actor code ID for actors version %d", av)
	}

	// For IPC and spacenet, rewards are handled by the IPC gateway in subnets,
	// let's not allocate any initial balance into the reward actor if this is not
	// the rootnet.
	balance := abi.NewTokenAmount(0)
	if networkName == ipctypes.RootSubnet.String() {
		balance = types.BigInt{Int: build.InitialRewardBalance}
	}

	act := &types.Actor{
		Code:    actcid,
		Balance: balance,
		Head:    statecid,
	}

	return act, nil
}
