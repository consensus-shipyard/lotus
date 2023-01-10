package global

// This file is an ugly hack to replace the deprecated build.Consensus constant, which
// was used as throughout the code in conditional expressions that execute depending on
// its value. Ideally, we would be not depend  on a global variable for conditional code
// execution, but refactoring the code to avoid that is out of our current scope.
// TODO: refactor code to avoid the need for this
var injectedConsensusAlgorithm = None

type ConsensusAlgorithm int

const (
	None ConsensusAlgorithm = iota
	ExpectedConsensus
	MirConsensus
	TSPoWConsensus
)

func SetConsensusAlgorithm(algorithm ConsensusAlgorithm) {
	//if injectedConsensusAlgorithm != None {
	//	panic("consensus algorithm can only be set once")
	//}
	injectedConsensusAlgorithm = algorithm
}

func IsConsensusAlgorithm(algorithm ConsensusAlgorithm) bool {
	//return injectedConsensusAlgorithm == algorithm
	return algorithm == MirConsensus
}
