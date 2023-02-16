package ipc

import (
	"bytes"
	"context"
	"time"

	"github.com/consensus-shipyard/go-ipc-types/sdk"
	subnetactor "github.com/consensus-shipyard/go-ipc-types/subnetactor"
	"go.uber.org/fx"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	init_ "github.com/filecoin-project/go-state-types/builtin/v10/init"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/impl/full"
)

type IpcAPI struct {
	fx.In

	full.MpoolAPI
	full.StateAPI
}

func (a *IpcAPI) IpcAddSubnetActor(ctx context.Context, wallet address.Address, params subnetactor.ConstructParams) (address.Address, error) {
	// override parent net to reflect the current network version
	// TODO: Instead of accept the ConstructorParams directly, we could receive
	// the individual arguments or a subset of the params struct to avoid having
	// to perform this replacement.
	netName, err := a.StateAPI.StateNetworkName(ctx)
	if err != nil {
		return address.Undef, err
	}
	params.Parent, err = sdk.NewSubnetIDFromString(string(netName))
	if err != nil {
		return address.Undef, err
	}

	// serialize constructor params to pass them to the exec message
	constParams, err := actors.SerializeParams(&params)
	if err != nil {
		return address.Undef, err
	}

	// get network version to retrieve the CodeCid of the subnet-actor
	nv, err := a.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return address.Undef, err
	}
	codeCid, err := a.StateActorCodeCIDs(ctx, nv)
	if err != nil {
		return address.Undef, err
	}
	initParams, err := actors.SerializeParams(&init_.ExecParams{
		CodeCID:           codeCid[subnetactor.ManifestID],
		ConstructorParams: constParams,
	})
	if err != nil {
		return address.Undef, err
	}

	smsg, aerr := a.MpoolPushMessage(ctx, &types.Message{
		To:     builtin.InitActorAddr,
		From:   wallet,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MethodsInit.Exec,
		Params: initParams,
	}, nil)
	if aerr != nil {
		return address.Undef, aerr
	}

	// this api call is sync and waits for the message to go through, we are adding a
	// timeout to avoid it getting stuck.
	// TODO: This API rpc is currently for testing purposes and for its use through the cli
	// if we end up adopting this method as critical for the operation of IPC we should
	// probably make it async and let the user determine if the deployment was successful or not.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	recpt, aerr := a.StateWaitMsg(ctx, smsg.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
	if aerr != nil {
		return address.Undef, aerr
	}
	r := &init_.ExecReturn{}
	if err := r.UnmarshalCBOR(bytes.NewReader(recpt.Receipt.Return)); err != nil {
		return address.Undef, err
	}
	return r.IDAddress, nil
}
