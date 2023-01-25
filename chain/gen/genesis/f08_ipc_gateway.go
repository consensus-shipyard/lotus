package genesis

import (
	"context"

	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"

	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	ipctypes "github.com/consensus-shipyard/go-ipc-types/types"
)

const bitWidth = 5;
const minStake = 1000000000000000000;

func constructState(store adt.Store, networkName string, checkPeriod int64) (*ipctypes.IPCGatewayState, error) {
	emptyArrayCid, err := adt.StoreEmptyArray(store, bitWidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}

	emptyMapCid, err := adt.StoreEmptyMap(store, bitWidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}

	network, err := ipctypes.NewSubnetIDFromString(networkName)
	if err != nil {
		return nil, xerrors.Errorf("cannot parse network name: %w", err)
	}

	return &ipctypes.IPCGatewayState{
		NetworkName:          *network,
		TotalSubnets:         0,
		MinStake:             big.NewInt(minStake),
		Subnets:              emptyMapCid,
		CheckPeriod:          ipctypes.ChainEpoch(checkPeriod),
		Checkpoints:          emptyMapCid,
		CheckMsgRegistry:     emptyMapCid,
		Nonce:                0,
		BottomupNonce:        0,
		BottomupMsgMeta:      emptyArrayCid,
		AppliedBottomupNonce: 18446744073709551615,
		AppliedTopdownNonce:  0,
	}, nil
}

func SetupIPCGateway(ctx context.Context, bs bstore.Blockstore, av actorstypes.Version, networkName string, checkPeriod int64) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	dst, err := constructState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), networkName, checkPeriod)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, dst)
	if err != nil {
		return nil, err
	}

	// generated from rust bundle and coded, should be correct
	actcid, _ := cid.Decode("bafk2bzaceazqq57zwvfufy66tttef4c5agib53scrmygaytjp5iolzpo6uby4")

	act := &types.Actor{
		Code:    actcid,
		Head:    statecid,
		Balance: big.Zero(),
	}

	return act, nil
}
