package mir

import (
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
	"github.com/filecoin-project/mir/pkg/checkpoint"

	"github.com/filecoin-project/lotus/chain/ipcagent/rpc"
)

// ---

type BaseConfig struct {
	// Validator address.
	Addr address.Address
	// Persistent storage file path.
	DatastorePath string
	// InitialCheckpoint from which to start the validator.
	InitialCheckpoint *checkpoint.StableCheckpoint
	// CheckpointRepo determines the path where Mir checkpoints
	// will be (optionally) persisted.
	CheckpointRepo string
	// The name of the group of validators.
	GroupName string
	// The source of membership: file, chain, etc.
	MembershipSourceValue string
}

const (
	DefaultMembershipSource = "file"
	// DefaultConfigOffset is the default number of epochs by which to delay configuration changes.
	// If a configuration is agreed upon in epoch e, it will take effect in epoch e + 1 + configOffset.
	DefaultConfigOffset           = 2
	DefaultMaxBlockDelay          = time.Duration(1)
	DefaultSegmentLength          = 1
	DefaultMaxTransactionsInBatch = 1024
)

type ConsensusConfig struct {
	// The length of an ISS segment in Mir, in sequence numbers. Must not be negative.
	SegmentLength          int
	MaxProposeDelay        time.Duration
	ConfigOffset           int
	MaxTransactionsInBatch int
}

// ---

type Config struct {
	*BaseConfig

	IPCAgent *rpc.Config

	Consensus *ConsensusConfig
}

func NewConfig(
	addr address.Address,
	dbPath string,
	initCheck *checkpoint.StableCheckpoint,
	checkpointRepo string,
	segmentLength, configOffset int,
	maxBlockDelay time.Duration,
	rpcServerURL string,
	membershipSource string,

) *Config {
	if !membership.IsSourceValid(membershipSource) {
		membershipSource = DefaultMembershipSource
	}

	base := BaseConfig{
		Addr:                  addr,
		DatastorePath:         dbPath,
		InitialCheckpoint:     initCheck,
		CheckpointRepo:        checkpointRepo,
		MembershipSourceValue: membershipSource,
	}

	if maxBlockDelay <= 0 {
		maxBlockDelay = DefaultMaxBlockDelay
	}
	if configOffset <= 0 {
		configOffset = DefaultConfigOffset
	}
	if segmentLength <= 0 {
		segmentLength = DefaultSegmentLength
	}
	cns := ConsensusConfig{
		SegmentLength:          segmentLength,
		ConfigOffset:           configOffset,
		MaxProposeDelay:        maxBlockDelay,
		MaxTransactionsInBatch: DefaultMaxTransactionsInBatch,
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
