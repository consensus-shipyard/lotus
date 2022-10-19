package main

import (
	gen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/lotus/chain/consensus/mir"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "mir",
		mir.Validator{},
		mir.ValidatorSet{},
		mir.Checkpoint{},
		mir.CheckpointData{},
		mir.ParentMeta{},
		mir.EpochConfig{},
		mir.CertSig{},
		mir.Membership{},
		mir.MembershipInfo{},
	); err != nil {
		panic(err)
	}
}
