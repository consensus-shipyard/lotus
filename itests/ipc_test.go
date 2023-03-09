package itests

import (
	"context"
	"testing"

	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/subnetactor"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/chain/gen/genesis"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

// TestIPCAccessorsNoErr tests all the basic accessors after just initializing
// the IPC actors to double-check that the basic serialization between Go
// and Rust works
//
// The only check from this test is that the accessors do not fail or that
// if they do so they do it in a predictable way.
func TestIPCEmptyAccessorsNoErr(t *testing.T) {
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
		Parent:            parent,
		Name:              "test",
		IPCGatewayAddr:    genesis.DefaultIPCGatewayAddrID,
		CheckPeriod:       genesis.DefaultCheckpointPeriod,
		FinalityThreshold: 5,
		MinValidators:     1,
		MinValidatorStake: abi.TokenAmount(types.MustParseFIL("1FIL")),
		Consensus:         subnetactor.Mir,
	}
	actorAddr, err := api.IPCAddSubnetActor(ctx, src, params)
	require.NoError(t, err)
	sn, err := sdk.NewSubnetIDFromString("/root/" + actorAddr.String())

	// get subnet actor state
	_, err = api.IPCReadSubnetActorState(ctx, sn, types.EmptyTSK)
	require.NoError(t, err)
	// get votes for checkpoints.
	_, err = api.IPCGetVotesForCheckpoint(ctx, sn, cid.Undef)
	require.NoError(t, err)
	// get checkpoint for epoch in the future returns an error because
	// no checkpoint is committed
	_, err = api.IPCGetCheckpoint(ctx, sn, 100)
	require.Error(t, err)
	// get empty checkpoint template
	_, err = api.IPCGetCheckpointTemplate(ctx, genesis.DefaultIPCGatewayAddr, 0)
	require.NoError(t, err)
	// get list of child subnets and see there are none
	l, err := api.IPCListChildSubnets(ctx, genesis.DefaultIPCGatewayAddr)
	require.NoError(t, err)
	require.Equal(t, len(l), 0)
	// get previous checkpoint for child
	require.NoError(t, err)
	// getting child checkpoint for the subnet fails because it has not been
	// registered
	_, err = api.IPCGetPrevCheckpointForChild(ctx, genesis.DefaultIPCGatewayAddr, sn)
	require.Error(t, err)
}
