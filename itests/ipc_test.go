package itests

import (
	"context"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/consensus-shipyard/go-ipc-types/gateway"
	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/subnetactor"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

// TestIPCAccessors lightly tests all the basic IPC accessors
// to double-check that the basic serialization between Go
// and Rust works. Do not treat this as a proper end-to-end test
// but just as a sanity-check that the basic integration works.
func TestIPCAccessors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	defer func() {
		t.Logf("[*] defer: cancelling %s context", t.Name())
		cancel()
		err := g.Wait()
		require.NoError(t, err)
		t.Logf("[*] defer: system %s stopped", t.Name())
	}()

	nodes, validators, ens := kit.EnsembleWithMirValidators(t, 1)
	ens.BeginMirMining(ctx, g, validators...)

	// check gateway state
	api := nodes[0]
	_, err := api.IPCReadGatewayState(ctx, genesis.DefaultIPCGatewayAddr, types.EmptyTSK)
	require.NoError(t, err)

	// add subnet actor
	src, err := api.WalletDefaultAddress(ctx)
	require.NoError(t, err)
	networkName, err := api.StateNetworkName(ctx)
	require.NoError(t, err)
	parent, err := sdk.NewSubnetIDFromString(string(networkName))
	require.NoError(t, err)

	params := subnetactor.ConstructParams{
		Parent:              parent,
		Name:                "test",
		IPCGatewayAddr:      genesis.DefaultIPCGatewayAddrID,
		BottomUpCheckPeriod: genesis.DefaultCheckpointPeriod,
		TopDownCheckPeriod:  genesis.DefaultCheckpointPeriod,
		MinValidators:       1,
		MinValidatorStake:   abi.TokenAmount(types.MustParseFIL("1FIL")),
		Consensus:           subnetactor.Mir,
	}
	actorAddr, err := api.IPCAddSubnetActor(ctx, src, params)
	require.NoError(t, err)
	sn, err := sdk.NewSubnetIDFromString("/root/" + actorAddr.String())
	require.NoError(t, err)

	JoinSubnet(t, ctx, api, src, actorAddr)

	// get subnet actor state
	_, err = api.IPCReadSubnetActorState(ctx, sn, types.EmptyTSK)
	require.NoError(t, err)

	checkEpoch := abi.ChainEpoch(genesis.DefaultCheckpointPeriod)
	c, err := abi.CidBuilder.Sum([]byte("genesis"))
	require.NoError(t, err)
	SubmitCheckpoint(t, ctx, api, sn, src, checkEpoch, c)

	// get list of child subnets and see there are none
	l, err := api.IPCListChildSubnets(ctx, genesis.DefaultIPCGatewayAddr)
	require.NoError(t, err)
	require.Equal(t, len(l), 1)
	// get checkpoint for epoch
	_, err = api.IPCGetCheckpoint(ctx, sn, checkEpoch)
	require.NoError(t, err)
	// get empty checkpoint template
	_, err = api.IPCGetCheckpointTemplate(ctx, genesis.DefaultIPCGatewayAddr, 0)
	require.NoError(t, err)
	// get previous checkpoint for child
	_, err = api.IPCGetPrevCheckpointForChild(ctx, genesis.DefaultIPCGatewayAddr, sn)
	require.NoError(t, err)
	// get list of checkpoints
	chs, err := api.IPCListCheckpoints(ctx, sn, 0, 2*genesis.DefaultCheckpointPeriod)
	require.NoError(t, err)
	require.Equal(t, len(chs), 1)
}

func JoinSubnet(t *testing.T, ctx context.Context, node *kit.TestFullNode, from, snActor address.Address) {
	params, err := actors.SerializeParams(&subnetactor.JoinParams{ValidatorNetAddr: "test"})
	require.NoError(t, err)
	smsg, aerr := node.MpoolPushMessage(ctx, &types.Message{
		To:     snActor,
		From:   from,
		Value:  abi.TokenAmount(types.MustParseFIL("10")),
		Method: builtin.MustGenerateFRCMethodNum("Join"),
		Params: params,
	}, nil)
	require.NoError(t, aerr)

	_, aerr = node.StateWaitMsg(ctx, smsg.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
	require.NoError(t, aerr)

}

func SubmitCheckpoint(t *testing.T, ctx context.Context, node *kit.TestFullNode, sn sdk.SubnetID, from address.Address, epoch abi.ChainEpoch, prev cid.Cid) {
	ch := gateway.NewBottomUpCheckpoint(sn, epoch)
	ch.Data.PrevCheck = prev
	params, err := actors.SerializeParams(ch)
	require.NoError(t, err)
	smsg, aerr := node.MpoolPushMessage(ctx, &types.Message{
		To:     sn.Actor,
		From:   from,
		Value:  abi.NewTokenAmount(0),
		Method: builtin.MustGenerateFRCMethodNum("SubmitCheckpoint"),
		Params: params,
	}, nil)
	require.NoError(t, aerr)

	_, aerr = node.StateWaitMsg(ctx, smsg.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
	require.NoError(t, aerr)
}
