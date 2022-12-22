package validator

type MembershipReader interface {
	GetValidators() (*ValidatorSet, error)
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

// GetValidators gets the membership config from a file.
func (f FileMembership) GetValidators() (*ValidatorSet, error) {
	return NewValidatorSetFromFile(f.FileName)
}

// ------

type StringMembership string

// GetValidators gets the membership config from the input string.
func (s StringMembership) GetValidators() (*ValidatorSet, error) {
	return NewValidatorsFromString(string(s))
}

// -----

type EnvMembership string

// GetValidators gets the membership config from the input environment variable.
func (e EnvMembership) GetValidators() (*ValidatorSet, error) {
	return NewValidatorsFromEnv(string(e))
}
