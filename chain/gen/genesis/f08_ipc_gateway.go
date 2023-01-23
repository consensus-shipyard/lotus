package genesis

import (
	"context"

	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/manifest"
	"github.com/filecoin-project/specs-actors/actors/util/adt"

	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/datacap"
	"github.com/filecoin-project/lotus/chain/types"
)

type State struct {
	// TODO: where is SubnetID type?
	networkName           SubnetID
    totalSubnets          uint64
    minStake              abi.TokenAmount
    subnets               cid.Cid
    checkPeriod           int64
    checkpoints           cid.Cid
    checkMsgRegistry      cid.Cid
	nonce                 u64
    bottomupNonce         u64
    bottomupMsgMeta       cid.Cid
	appliedBottomupNonce  u64
    appliedTopdownNonce   u64
}


func constructState(store adt.Store, networkName string, checkPeriod u64) (*State, error) {
	emptyArrayCid := adt.MakeEmptyArray(store)
	emptyMapCid, err := adt.StoreEmptyMap(store, int(bitWidth))
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}

	return &State{
		networkName:          networkName,
		totalSubnets:         0,
		minStake:             1000000000000000000,
		subnets:              emptyMapCid,
		checkPeriod:          checkPeriod,
		checkpoints:          emptyMapCid,
		checkMsgRegistry:     emptyMapCid,
		nonce:                0,
		bottomupNonce:        0,
		bottomupMsgMeta:      emptyArrayCid,
		appliedBottomupNonce: 18446744073709551615,
		appliedTopdownNonce:  0,
	}, nil
}

func SetupIPCGateway(ctx context.Context, bs bstore.Blockstore, av actorstypes.Version, networkName string, checkPeriod u64) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	dst, err := constructState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), networkName, checkPeriod)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, dst.GetState())
	if err != nil {
		return nil, err
	}

	actcid, ok := actors.GetActorCodeID(av, cid.Cid.from("bafk2bzaceazqq57zwvfufy66tttef4c5agib53scrmygaytjp5iolzpo6uby4"))
	if !ok {
		return nil, xerrors.Errorf("failed to get datacap actor code ID for actors version %d", av)
	}

	act := &types.Actor{
		Code:    actcid,
		Head:    statecid,
		Balance: big.Zero(),
	}

	return act, nil
}
