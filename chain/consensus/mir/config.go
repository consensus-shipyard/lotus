package mir

import (
	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/mir/pkg/checkpoint"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
)

// ---

type BaseConfig struct {
	ID            address.Address
	DatastorePath string
	// InitialCheckpoint from which to start the validator.
	InitialCheckpoint *checkpoint.StableCheckpoint
	// CheckpointRepo determines the path where Mir checkpoints
	// will be (optionally) persisted.
	CheckpointRepo string
	// The name of the group of validators.
	GroupName string
}

type ConsensusConfig struct {
	// The length of an ISS segment in Mir, in sequence numbers. Must not be negative.
	SegmentLength int
}

// ---

type Config struct {
	*BaseConfig

	IPCAgent *rpc.Config

	Consensus *ConsensusConfig
}

func NewConfig(
	validatorID address.Address,
	dbPath string,
	initCheck *checkpoint.StableCheckpoint,
	checkpointRepo string,
	segmentLength int,
	rpcServerURL string,
) *Config {
	base := BaseConfig{
		ID:                validatorID,
		DatastorePath:     dbPath,
		InitialCheckpoint: initCheck,
		CheckpointRepo:    checkpointRepo,
	}
	cns := ConsensusConfig{
		SegmentLength: segmentLength,
	}

	cfg := Config{
		BaseConfig: &base,
		IPCAgent:   rpc.NewConfig(rpcServerURL),
		Consensus:  &cns,
	}

	return &cfg
}

func (cfg *Config) IPCConfig() *rpc.Config {
	return cfg.IPCAgent
}
