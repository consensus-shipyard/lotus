package manifest

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Manifest struct {
	Size        int    `toml:"size"`
	ConfigNonce int    `toml:"configNonce"`
	StartIP     string `toml:"startIP"`
	ConfigDir   string `toml:"configDir"`
}

func (m Manifest) Save(file string) error {
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("failed to create manifest file %q: %w", file, err)
	}
	return toml.NewEncoder(f).Encode(m)
}

// LoadManifest loads a testnet manifest from a file.
func LoadManifest(file string) (Manifest, error) {
	manifest := Manifest{}
	_, err := toml.DecodeFile(file, &manifest)
	if err != nil {
		return manifest, fmt.Errorf("failed to load testnet manifest %q: %w", file, err)
	}
	return manifest, nil
}
