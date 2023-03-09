package genesis

import (
	"context"

	"github.com/consensus-shipyard/go-ipc-types/gateway"
	ipctypes "github.com/consensus-shipyard/go-ipc-types/sdk"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	actorstypes "github.com/filecoin-project/go-state-types/actors"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/specs-actors/v7/actors/util/adt"

	bstore "github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	// Default checkpoint period for the IPC gateway.
	DefaultCheckpointPeriod = 10
	DefaultIPCGatewayAddrID = 64

	bitWidth  = 5
	minStake  = 1000000000000000000
	MaxUint64 = ^uint64(0)
)

var (
	// DefaultIPCGatewayAddr used to deploy the gateway in genesis.
	DefaultIPCGatewayAddr, _ = address.NewIDAddress(DefaultIPCGatewayAddrID)
)

func constructState(store adt.Store, network ipctypes.SubnetID, checkPeriod int64) (*gateway.State, error) {
	emptyArrayCid, err := adt.StoreEmptyArray(store, bitWidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}

	emptyMapCid, err := adt.StoreEmptyMap(store, bitWidth)
	if err != nil {
		return nil, xerrors.Errorf("failed to create empty map: %w", err)
	}

	return &gateway.State{
		NetworkName:          network,
		TotalSubnets:         0,
		MinStake:             big.NewInt(minStake),
		Subnets:              emptyMapCid,
		CheckPeriod:          abi.ChainEpoch(checkPeriod),
		Checkpoints:          emptyMapCid,
		CheckMsgRegistry:     emptyMapCid,
		Postbox:              emptyMapCid,
		Nonce:                0,
		BottomupNonce:        0,
		BottomupMsgMeta:      emptyArrayCid,
		AppliedBottomupNonce: MaxUint64,
		AppliedTopdownNonce:  0,
	}, nil
}

func SetupIPCGateway(ctx context.Context, bs bstore.Blockstore, av actorstypes.Version, networkName string, checkPeriod int64) (*types.Actor, error) {
	cst := cbor.NewCborStore(bs)
	network, err := ipctypes.NewSubnetIDFromString(networkName)
	if err != nil {
		return nil, xerrors.Errorf("cannot parse network name as subnetID: %w", err)
	}

	dst, err := constructState(adt.WrapStore(ctx, cbor.NewCborStore(bs)), network, checkPeriod)
	if err != nil {
		return nil, err
	}

	statecid, err := cst.Put(ctx, dst)
	if err != nil {
		return nil, err
	}

	actcid, ok := actors.GetActorCodeID(av, gateway.ManifestID)
	if !ok {
		return nil, xerrors.Errorf("failed to get ipc-gateway actor code ID for actors version %d", av)
	}

	// the gateway receives the same initial balance as the reward actor, this is used
	// to mint new tokens in subnets when top-down messages are executed.
	// This balance is zero in the root, as now top-down messages can be executed in the root.
	balance := abi.NewTokenAmount(0)
	if network != ipctypes.RootSubnet {
		balance = types.BigInt{Int: build.InitialRewardBalance}
	}

	act := &types.Actor{
		Code:    actcid,
		Head:    statecid,
		Balance: balance,
	}

	return act, nil
}
