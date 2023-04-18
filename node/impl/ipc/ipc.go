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
	} else {
		ts = a.Chain.GetHeaviestTipSet()
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
	if sn.PrevCheckpoint != nil {
		return sn.PrevCheckpoint.Cid()
	}
	return cid.Undef, nil
}

// IPCGetCheckpointTemplate to be populated and signed for the epoch given as input.
// If the template for the epoch is empty (either because it has no data or an epoch from the
// future was provided) an empty template is returned.
func (a *IPCAPI) IPCGetCheckpointTemplate(ctx context.Context, gw address.Address, epoch abi.ChainEpoch) (*gateway.BottomUpCheckpoint, error) {
	st, err := a.IPCReadGatewayState(ctx, gw, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	return st.GetWindowCheckpoint(a.Chain.ActorStore(ctx), epoch)
}

// IPCGetCheckpointTemplateSerialized returns a cbor serialization of the template so the same
// serialization used in actor state can be used to deserialize the checkpoint without
// intermediate representations.
func (a *IPCAPI) IPCGetCheckpointTemplateSerialized(ctx context.Context, gw address.Address, epoch abi.ChainEpoch) ([]byte, error) {
	c, err := a.IPCGetCheckpointTemplate(ctx, gw, epoch)
	if err != nil {
		return nil, err
	}
	// NOTE: Templates use cid.Undef for prevCheck because validators need to populate
	// the value. We can't serialize a cid.Undef, so we set a sample Cid here to
	// allow the serialization. This cid shouldn't be considered as a valid previous checkpoint,
	// or things may break.
	if c.Data.PrevCheck == cid.Undef {
		dummyCid, _ := cid.Parse("bafkqaaa")
		c.Data.PrevCheck = dummyCid
	}
	buf := new(bytes.Buffer)
	if err := c.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// IPCHasVotedBottomUpCheckpoint checks if a validator has already voted a specific checkpoint
// for certain epoch
func (a *IPCAPI) IPCHasVotedBottomUpCheckpoint(ctx context.Context, sn sdk.SubnetID, e abi.ChainEpoch, v address.Address) (bool, error) {
	if err := a.checkParent(ctx, sn); err != nil {
		return false, err
	}
	st, err := a.IPCReadSubnetActorState(ctx, sn, types.EmptyTSK)
	if err != nil {
		return false, err
	}
	// ValidatorHasVoted expects on-chain IDs. This is how votes are indexed in the StateAPI
	// of the actor.
	v, err = a.StateManager.LookupID(ctx, v, a.Chain.GetHeaviestTipSet())
	if err != nil {
		return false, xerrors.Errorf("error getting on-chain ID for validator: %w", err)
	}
	return st.BottomUpCheckpointVoting.ValidatorHasVoted(a.Chain.ActorStore(ctx), e, v)
}

// IPCHasVotedTopDownCheckpoint checks if a validator has already voted a specific checkpoint
// for certain epoch
func (a *IPCAPI) IPCHasVotedTopDownCheckpoint(ctx context.Context, gw address.Address, e abi.ChainEpoch, v address.Address) (bool, error) {
	st, err := a.IPCReadGatewayState(ctx, gw, types.EmptyTSK)
	if err != nil {
		return false, err
	}
	// ValidatorHasVoted expects on-chain IDs. This is how votes are indexed in the StateAPI
	// of the actor.
	v, err = a.StateManager.LookupID(ctx, v, a.Chain.GetHeaviestTipSet())
	if err != nil {
		return false, xerrors.Errorf("error getting on-chain ID for validator: %w", err)
	}
	return st.TopDownCheckpointVoting.ValidatorHasVoted(a.Chain.ActorStore(ctx), e, v)
}

// IPCListCheckpoints returns a list of checkpoints committed for a submit between two epochs
func (a *IPCAPI) IPCListCheckpoints(ctx context.Context, sn sdk.SubnetID, from, to abi.ChainEpoch) ([]*gateway.BottomUpCheckpoint, error) {
	if err := a.checkParent(ctx, sn); err != nil {
		return nil, err
	}
	st, err := a.IPCReadSubnetActorState(ctx, sn, types.EmptyTSK)
	if err != nil {
		return nil, err
	}
	// get the first epoch with checkpoints after the from.
	i := gateway.CheckpointEpoch(from, st.BottomUpCheckPeriod)
	out := make([]*gateway.BottomUpCheckpoint, 0)
	for i <= to {
		ch, found, err := st.GetCheckpoint(a.Chain.ActorStore(ctx), i)
		if err != nil {
			return nil, xerrors.Errorf("error getting checkpoint from actor store in epoch %d: %w", i, err)
		}
		if found {
			out = append(out, ch)
		}
		i += st.BottomUpCheckPeriod
	}
	return out, nil
}

// IPCListCheckpointsSerialized returns a list of checkpoints committed for a submit between two epochs
// where each checkpoint is conveniently CBOR serialized.
func (a *IPCAPI) IPCListCheckpointsSerialized(ctx context.Context, sn sdk.SubnetID, from, to abi.ChainEpoch) ([][]byte, error) {
	l, err := a.IPCListCheckpoints(ctx, sn, from, to)
	if err != nil {
		return nil, err
	}

	out := make([][]byte, 0)
	for _, c := range l {
		buf := new(bytes.Buffer)
		if err := c.MarshalCBOR(buf); err != nil {
			return nil, err
		}
		out = append(out, buf.Bytes())
	}
	return out, nil
}

// IPCGetCheckpoint returns the checkpoint committed in the subnet actor for an epoch.
func (a *IPCAPI) IPCGetCheckpoint(ctx context.Context, sn sdk.SubnetID, epoch abi.ChainEpoch) (*gateway.BottomUpCheckpoint, error) {
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

// IPCGetCheckpointSerialized returns the checkpoint committed in the subnet actor for an epoch.
func (a *IPCAPI) IPCGetCheckpointSerialized(ctx context.Context, sn sdk.SubnetID, epoch abi.ChainEpoch) ([]byte, error) {
	c, err := a.IPCGetCheckpoint(ctx, sn, epoch)
	if err != nil {
		return nil, err
	}
	buf := new(bytes.Buffer)
	if err := c.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

// IPCGetTopDownMsgs returns the list of top down-messages from a specific nonce
// to the latest one that has been committed in the subnet.
func (a *IPCAPI) IPCGetTopDownMsgs(ctx context.Context, gatewayAddr address.Address, sn sdk.SubnetID, tsk types.TipSetKey, nonce uint64) ([]*gateway.CrossMsg, error) {
	st, err := a.IPCReadGatewayState(ctx, gatewayAddr, tsk)
	if err != nil {
		return nil, err
	}
	subnet, found, err := st.GetSubnet(a.Chain.ActorStore(ctx), sn)
	if err != nil {
		return nil, xerrors.Errorf("error getting subnet: %w", err)
	}
	if !found {
		return nil, xerrors.Errorf("subnet not found in gateway")
	}
	return subnet.TopDownMsgsFromNonce(a.Chain.ActorStore(ctx), nonce)
}

// IPCGetTopDownMsgsSerialized returns the list of top down-messages
// cbor serialized
func (a *IPCAPI) IPCGetTopDownMsgsSerialized(ctx context.Context, gatewayAddr address.Address, sn sdk.SubnetID, tsk types.TipSetKey, nonce uint64) ([][]byte, error) {
	l, err := a.IPCGetTopDownMsgs(ctx, gatewayAddr, sn, tsk, nonce)
	if err != nil {
		return nil, err
	}

	out := make([][]byte, 0)
	for _, c := range l {
		buf := new(bytes.Buffer)
		if err := c.MarshalCBOR(buf); err != nil {
			return nil, err
		}
		out = append(out, buf.Bytes())
	}
	return out, nil
}

// IPCGetGenesisEpochForSubnet returns the genesis epoch from which a subnet has been
// registered in the parent.
func (a *IPCAPI) IPCGetGenesisEpochForSubnet(ctx context.Context, gatewayAddr address.Address, sn sdk.SubnetID) (abi.ChainEpoch, error) {
	st, err := a.IPCReadGatewayState(ctx, gatewayAddr, types.EmptyTSK)
	if err != nil {
		return 0, err
	}
	subnet, found, err := st.GetSubnet(a.Chain.ActorStore(ctx), sn)
	if err != nil {
		return 0, xerrors.Errorf("error getting subnet: %w", err)
	}
	if !found {
		return 0, xerrors.Errorf("subnet not found in gateway")
	}
	return subnet.GenesisEpoch, nil
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
