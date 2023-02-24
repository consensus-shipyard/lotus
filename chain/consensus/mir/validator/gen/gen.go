package main

import (
	gen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/lotus/chain/consensus/mir/validator"
)

func main() {
	if err := gen.WriteTupleEncodersToFile("./cbor_gen.go", "validator",
		validator.Validator{},
		validator.Set{},
	); err != nil {
		panic(err)
	}
}
