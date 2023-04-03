package itests

import (
	"context"
	"encoding/binary"
	"testing"
	"unicode"

	"github.com/consensus-shipyard/go-ipc-types/gateway"
	"github.com/consensus-shipyard/go-ipc-types/sdk"
	"github.com/consensus-shipyard/go-ipc-types/subnetactor"
	"github.com/ipfs/go-cid"
	"github.com/minio/blake2b-simd"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

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
	require.NoError(t, err)

	JoinSubnet(t, ctx, api, src, actorAddr)
	c, err := abi.CidBuilder.Sum([]byte("genesis"))
	require.NoError(t, err)
	SubmitCheckpoint(t, ctx, api, sn, src, 100, c)

	// get subnet actor state
	_, err = api.IPCReadSubnetActorState(ctx, sn, types.EmptyTSK)
	require.NoError(t, err)
	// get votes for checkpoints.
	_, err = api.IPCGetVotesForCheckpoint(ctx, sn, cid.Undef)
	require.NoError(t, err)
	// get list of child subnets and see there are none
	l, err := api.IPCListChildSubnets(ctx, genesis.DefaultIPCGatewayAddr)
	require.NoError(t, err)
	require.Equal(t, len(l), 1)
	// get checkpoint for epoch
	_, err = api.IPCGetCheckpoint(ctx, sn, 100)
	require.NoError(t, err)
	// get empty checkpoint template
	_, err = api.IPCGetCheckpointTemplate(ctx, genesis.DefaultIPCGatewayAddr, 0)
	require.NoError(t, err)
	// get previous checkpoint for child
	_, err = api.IPCGetPrevCheckpointForChild(ctx, genesis.DefaultIPCGatewayAddr, sn)
	require.NoError(t, err)
	// get list of checkpoints
	chs, err := api.IPCListCheckpoints(ctx, sn, 0, 100)
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
		Method: MustGenerateFRCMethodNum("Join"),
		Params: params,
	}, nil)
	require.NoError(t, aerr)

	_, aerr = node.StateWaitMsg(ctx, smsg.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
	require.NoError(t, aerr)

}

func SubmitCheckpoint(t *testing.T, ctx context.Context, node *kit.TestFullNode, sn sdk.SubnetID, from address.Address, epoch abi.ChainEpoch, prev cid.Cid) {
	ch := gateway.NewCheckpoint(sn, epoch)
	ch.Data.PrevCheck = prev
	params, err := actors.SerializeParams(ch)
	require.NoError(t, err)
	smsg, aerr := node.MpoolPushMessage(ctx, &types.Message{
		To:     sn.Actor,
		From:   from,
		Value:  abi.NewTokenAmount(0),
		Method: MustGenerateFRCMethodNum("SubmitCheckpoint"),
		Params: params,
	}, nil)
	require.NoError(t, aerr)

	_, aerr = node.StateWaitMsg(ctx, smsg.Cid(), build.MessageConfidence, api.LookbackNoLimit, true)
	require.NoError(t, aerr)
}

// Generates a standard FRC-42 compliant method number
// Reference: https://github.com/filecoin-project/FIPs/blob/master/FRCs/frc-0042.md
// This code was borrowed from: https://github.com/filecoin-project/go-state-types/blob/master/builtin/frc_0042.go
// In the future consider using directly that library.
func GenerateFRCMethodNum(name string) (abi.MethodNum, error) {
	err := validateMethodName(name)
	if err != nil {
		return 0, err
	}

	digest := blake2b.Sum512([]byte("1|" + name))

	for i := 0; i < 64; i += 4 {
		methodId := binary.BigEndian.Uint32(digest[i : i+4])
		if methodId >= (1 << 24) {
			return abi.MethodNum(methodId), nil
		}
	}

	return abi.MethodNum(0), xerrors.Errorf("Could not generate method num from method name %s:", name)
}

func validateMethodName(name string) error {
	if name == "" {
		return xerrors.Errorf("empty name string")
	}

	if !(unicode.IsUpper(rune(name[0])) || name[0] == "_"[0]) {
		return xerrors.Errorf("Method name first letter must be uppercase or underscore, method name: %s", name)
	}

	for _, c := range name {
		if !(unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_') {
			return xerrors.Errorf("method name has illegal characters, method name: %s", name)
		}
	}

	return nil
}

func MustGenerateFRCMethodNum(name string) abi.MethodNum {
	methodNum, err := GenerateFRCMethodNum(name)
	if err != nil {
		panic(err)
	}
	return methodNum
}
