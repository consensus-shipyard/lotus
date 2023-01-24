package main

import (
	gen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/lotus/chain/consensus/mir"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "mir",
		mir.Checkpoint{},
		mir.ParentMeta{},
		mir.VoteMessage{},
	); err != nil {
		panic(err)
	}
}
