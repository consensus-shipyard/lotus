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
	nodesConfigDir := flag.String("nodes", "./testdata/mir/mir-config", "nodes config directory")
	genesisDir := flag.String("genesis", "./testdata/mir", "genesis directory")
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
		*nodesConfigDir = m.NodesConfigDir
		*genesisDir = m.GenesisDir
	}

	err := generator.SaveNewNetworkConfig(*n, *firstIP, *nonce, *nodesConfigDir, *genesisDir)
	if err != nil {
		panic(err)
	}
}
