package main

import (
	"flag"
	"fmt"

	"github.com/filecoin-project/lotus/e2e/generator"
	"github.com/filecoin-project/lotus/e2e/internal/manifest"
)

func main() {
	firstIP := flag.String("ip", "192.168.10.2", "IP address of the first validator node")
	n := flag.Int("n", 4, "number of nodes")
	nonce := flag.Int("nonce", 0, "configuration number")
	outputDir := flag.String("output", "./testdata/mir/", "output directory")
	manifestFile := flag.String("manifest", "", "use manifest file")

	flag.Parse()

	if *manifestFile != "" {
		m, err := manifest.LoadManifest(*manifestFile)
		if err != nil {
			panic(err)
		}
		fmt.Println(m)
		*firstIP = m.StartIP
		*n = m.Size
		*nonce = m.ConfigNonce
		*outputDir = m.ConfigDir
	}

	err := generator.SaveNewNetworkConfig(*n, *firstIP, *nonce, *outputDir)
	if err != nil {
		panic(err)
	}
}
