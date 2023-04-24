package main

import (
	"flag"
	"fmt"
	"path"

	"github.com/filecoin-project/lotus/e2e/generator"
)

func main() {
	firstIP := flag.String("ip", "192.168.10.2", "IP address of the first validator node")
	n := flag.Int("n", 4, "amount of node validators")
	nonce := flag.Int("nonce", 0, "configuration number")
	outputDir := flag.String("output", "./testdata/mir/", "output directory")

	flag.Parse()

	validatorSet, err := generator.NewValidatorSet(*firstIP, *n, *nonce)
	if err != nil {
		panic(err)
	}

	for i, v := range validatorSet.Validators {
		nodeConfigDir := path.Join(*outputDir, fmt.Sprintf("node%d", i))

		err = v.SaveToFile(nodeConfigDir)
		if err != nil {
			panic(err)
		}
	}

}
