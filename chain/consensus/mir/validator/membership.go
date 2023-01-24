package validator

type MembershipReader interface {
	GetValidatorSet() (*ValidatorSet, error)
}

// ------

type FileMembership struct {
	FileName string
}

func NewFileMembership(fileName string) FileMembership {
	return FileMembership{
		FileName: fileName,
	}
}

// GetValidatorSet gets the membership config from a file.
func (f FileMembership) GetValidatorSet() (*ValidatorSet, error) {
	return NewValidatorSetFromFile(f.FileName)
}

// ------

type StringMembership string

// GetValidatorSet gets the membership config from the input string.
func (s StringMembership) GetValidatorSet() (*ValidatorSet, error) {
	return NewValidatorSetFromString(string(s))
}

// -----

type EnvMembership string

// GetValidatorSet gets the membership config from the input environment variable.
func (e EnvMembership) GetValidatorSet() (*ValidatorSet, error) {
	return NewValidatorsFromEnv(string(e))
}
