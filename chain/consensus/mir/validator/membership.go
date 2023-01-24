package validator

type Reader interface {
	GetValidatorSet() (*Set, error)
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
func (f FileMembership) GetValidatorSet() (*Set, error) {
	return NewValidatorSetFromFile(f.FileName)
}

// ------

type StringMembership string

// GetValidatorSet gets the membership config from the input string.
func (s StringMembership) GetValidatorSet() (*Set, error) {
	return NewValidatorSetFromString(string(s))
}

// -----

type EnvMembership string

// GetValidatorSet gets the membership config from the input environment variable.
func (e EnvMembership) GetValidatorSet() (*Set, error) {
	return NewValidatorsFromEnv(string(e))
}
