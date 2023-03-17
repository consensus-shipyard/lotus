package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/consensus-shipyard/go-ipc-types/gateway"
	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/subnetactor"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"go.uber.org/fx"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	init_ "github.com/filecoin-project/go-state-types/builtin/v10/init"
	"github.com/filecoin-project/specs-actors/v2/actors/util/adt"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/eudico-core/genesis"
	"github.com/filecoin-project/lotus/node/impl/full"
)

type IPCAPI struct {
	fx.In

	full.MpoolAPI
	full.StateAPI
}

func (a *IPCAPI) IPCAddSubnetActor(ctx context.Context, wallet address.Address, params subnetactor.ConstructParams) (address.Address, error) {
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

func (a *IPCAPI) IPCReadGatewayState(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*gateway.State, error) {
	st := gateway.State{}
	if err := a.readActorState(ctx, actor, tsk, &st); err != nil {
		return nil, xerrors.Errorf("error getting gateway actor from StateStore: %w", err)
	}
	return &st, nil
}

func (a *IPCAPI) IPCReadSubnetActorState(ctx context.Context, sn sdk.SubnetID, tsk types.TipSetKey) (*subnetactor.State, error) {
	if err := a.checkParent(ctx, sn); err != nil {
		return nil, err
	}
	st := subnetactor.State{}
	if err := a.readActorState(ctx, sn.Actor, tsk, &st); err != nil {
		return nil, xerrors.Errorf("error getting subnet actor from StateStore: %w", err)
	}

	ts := new(types.TipSet)
	if tsk != types.EmptyTSK {
		tsCid, err := tsk.Cid()
		if err != nil {
			return nil, err
		}
		ts, err = a.Chain.GetTipSetByCid(ctx, tsCid)
		if err != nil {
			return nil, err
		}
	}

	// Resolve on-chain IDs of validators to f1/f3 addresses
	for i, v := range st.ValidatorSet.Validators {
		var err error
		st.ValidatorSet.Validators[i].Addr, err = a.StateManagerAPI.ResolveToDeterministicAddress(ctx, v.Addr, ts)
		if err != nil {
			return nil, err
		}

	}
	return &st, nil
}

// IPCGetPrevCheckpointForChild gets the latest checkpoint committed for a child subnet.
// This function is expected to be called in the parent of the checkpoint being populated.
// It inspects the state in the heaviest block (i.e. latest state available)
func (a *IPCAPI) IPCGetPrevCheckpointForChild(ctx context.Context, gatewayAddr address.Address, subnet sdk.SubnetID) (cid.Cid, error) {
	st, err := a.IPCReadGatewayState(ctx, gatewayAddr, types.EmptyTSK)
	if err != nil {
		return cid.Undef, err
	}
	sn, found, err := st.GetSubnet(adt.WrapStore(ctx, a.Chain.ActorStore(ctx)), subnet)
	if err != nil {
		return cid.Undef, xerrors.Errorf("error getting subnet from actor store: %w", err)
	}
	if !found {
		return cid.Undef, xerrors.Errorf("no subnet registered with id %s", subnet)
	}
	return sn.PrevCheckpoint.Cid()
}

// IPCGetCheckpointTemplate to be populated and signed for the epoch given as input.
// If the template for the epoch is empty (either because it has no data or an epoch from the
// future was provided) an empty template is returned.
func (a *IPCAPI) IPCGetCheckpointTemplate(ctx context.Context, gatewayAddr address.Address, epoch abi.ChainEpoch) (*gateway.Checkpoint, error) {
	st, err := a.IPCReadGatewayState(ctx, gatewayAddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	return st.GetWindowCheckpoint(a.Chain.ActorStore(ctx), epoch)
}

// IPCGetVotesForCheckpoint returns if there is an active voting for a checkpoint with a specific cid.
// If no active votings are found for a checkpoints is because the checkpoint has already been committed
// or because no one has votes that checkpoint yet.
func (a *IPCAPI) IPCGetVotesForCheckpoint(ctx context.Context, sn sdk.SubnetID, c cid.Cid) (*subnetactor.Votes, error) {
	if err := a.checkParent(ctx, sn); err != nil {
		return nil, err
	}
	st, err := a.IPCReadSubnetActorState(ctx, sn, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	v, found, err := st.GetCheckpointVotes(a.Chain.ActorStore(ctx), c)
	if err != nil {
		return nil, xerrors.Errorf("error getting votes from actor store: %w", err)
	}
	if !found {
		return &subnetactor.Votes{make([]address.Address, 0)}, nil
	}
	return v, nil
}

// IPCGetCheckpoint returns the checkpoint committed in the subnet actor for an epoch.
func (a *IPCAPI) IPCGetCheckpoint(ctx context.Context, sn sdk.SubnetID, epoch abi.ChainEpoch) (*gateway.Checkpoint, error) {
	if err := a.checkParent(ctx, sn); err != nil {
		return nil, err
	}
	st, err := a.IPCReadSubnetActorState(ctx, sn, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	ch, found, err := st.GetCheckpoint(a.Chain.ActorStore(ctx), epoch)
	if err != nil {
		return nil, xerrors.Errorf("error getting checkpoint from actor store: %w", err)
	}
	if !found {
		return nil, xerrors.Errorf("no checkpoint committed for epoch %v", epoch)
	}
	return ch, nil
}

// IPCSubnetGenesisTemplate returns a genesis template for a subnet. From this template
// peers in a subnet can deterministically generate the genesis block for the subnet.
func (a *IPCAPI) IPCSubnetGenesisTemplate(_ context.Context, subnet sdk.SubnetID) ([]byte, error) {
	// make genesis with default subnet template.
	// TODO: This will fail if eudico not run in the default path,
	// we should pass a path as an input or remove this endpoint.
	tmpl, err := genesis.MakeGenesisTemplate(subnet.String(), "")
	if err != nil {
		return nil, err
	}
	return json.Marshal(&tmpl)

}

// IPCListChildSubnets lists information about all child subnets registered as childs from the current one.
func (a *IPCAPI) IPCListChildSubnets(ctx context.Context, gatewayAddr address.Address) ([]gateway.Subnet, error) {
	st, err := a.IPCReadGatewayState(ctx, gatewayAddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	return st.ListSubnets(a.Chain.ActorStore(ctx))
}

// readActorState reads the state of a specific actor at a specefic epoch determined by the tipset key.
//
// The function accepts the address actor and the tipSetKet from which to read the state as an input, along
// with type variable where the state should be deserialized and stored. By passing the state object as an argument
// we signal the deserializer the type of the state. Passing the wrong state type for the actor
// being inspected leads to a deserialization error.
func (a *IPCAPI) readActorState(ctx context.Context, actor address.Address, tsk types.TipSetKey, stateType cbg.CBORUnmarshaler) error {
	ts, err := a.Chain.GetTipSetFromKey(ctx, tsk)
	if err != nil {
		return xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	act, err := a.StateManager.LoadActor(ctx, actor, ts)
	if err != nil {
		return xerrors.Errorf("getting actor: %w", err)
	}
	blk, err := a.Chain.StateBlockstore().Get(ctx, act.Head)
	if err != nil {
		return xerrors.Errorf("getting actor head: %w", err)
	}

	return stateType.UnmarshalCBOR(bytes.NewReader(blk.RawData()))

}

// check that the current network is the right parent for subnetID
func (a *IPCAPI) checkParent(ctx context.Context, sn sdk.SubnetID) error {
	netName, err := a.StateAPI.StateNetworkName(ctx)
	if err != nil {
		return err
	}

	if string(netName) != sn.Parent {
		return xerrors.Errorf("wrong subnet being called: the current network is not the parent of subnet provided: %s, %s",
			netName, sn.Parent)
	}
	return nil
}
