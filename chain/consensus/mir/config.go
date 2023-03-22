package mir

import (
	"time"

	"golang.org/x/xerrors"

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
	DefaultConfigOffset                 = 2
	DefaultMaxBlockDelay                = 1 * time.Second
	DefaultSegmentLength                = 1
	DefaultMaxTransactionsInBatch       = 1024
	DefaultPBFTViewChangeSNTimeout      = 6 * time.Second
	DefaultPBFTViewChangeSegmentTimeout = 6 * time.Second
)

type ConsensusConfig struct {
	// The length of an ISS segment in Mir, in sequence numbers. Must not be negative.
	SegmentLength                int
	ConfigOffset                 int
	MaxTransactionsInBatch       int
	MaxProposeDelay              time.Duration
	PBFTViewChangeSNTimeout      time.Duration
	PBFTViewChangeSegmentTimeout time.Duration
}

// ---

type Config struct {
	*BaseConfig

	IPCAgent *rpc.Config

	Consensus *ConsensusConfig
}

func DefaultConsensusConfig() *ConsensusConfig {
	return &ConsensusConfig{
		SegmentLength:                DefaultSegmentLength,
		ConfigOffset:                 DefaultConfigOffset,
		MaxTransactionsInBatch:       DefaultMaxTransactionsInBatch,
		MaxProposeDelay:              DefaultMaxBlockDelay,
		PBFTViewChangeSNTimeout:      DefaultPBFTViewChangeSNTimeout,
		PBFTViewChangeSegmentTimeout: DefaultPBFTViewChangeSegmentTimeout,
	}

}

func NewConfig(
	addr address.Address,
	dbPath string,
	initCheck *checkpoint.StableCheckpoint,
	checkpointRepo string,
	segmentLength, configOffset int,
	maxBlockDelayStr string,
	rpcServerURL string,
	membershipSource string,
) (*Config, error) {
	if !membership.IsSourceValid(membershipSource) {
		return nil, xerrors.Errorf("invalid membership source %s", membershipSource)
	}

	base := BaseConfig{
		Addr:                  addr,
		DatastorePath:         dbPath,
		InitialCheckpoint:     initCheck,
		CheckpointRepo:        checkpointRepo,
		MembershipSourceValue: membershipSource,
	}

	maxBlockDelay, err := time.ParseDuration(maxBlockDelayStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid max block delay string %s: %x", maxBlockDelayStr, err)
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
		SegmentLength:                segmentLength,
		ConfigOffset:                 configOffset,
		MaxProposeDelay:              maxBlockDelay,
		MaxTransactionsInBatch:       DefaultMaxTransactionsInBatch,
		PBFTViewChangeSNTimeout:      max(maxBlockDelay+5*time.Second, 6*time.Second),
		PBFTViewChangeSegmentTimeout: max((maxBlockDelay+2*time.Second)*time.Duration(segmentLength)+3*time.Second, 6*time.Second),
	}

	cfg := Config{
		BaseConfig: &base,
		IPCAgent:   rpc.NewConfig(rpcServerURL),
		Consensus:  &cns,
	}

	return &cfg, nil
}

func (cfg *Config) IPCConfig() *rpc.Config {
	return cfg.IPCAgent
}

func max(x, y time.Duration) time.Duration {
	if x < y {
		return y
	}
	return x
}
