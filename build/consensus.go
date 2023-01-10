package build

type ConsensusType int

const (
	FilecoinEC ConsensusType = 0
	Mir                      = 1
	PoW                      = 2
)

func IsMirConsensus() bool {
	return Consensus == Mir
}

func IsPoWConsensus() bool {
	return Consensus == PoW
}
