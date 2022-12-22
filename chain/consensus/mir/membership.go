package mir

type MembershipReader interface {
	GetValidators() (*ValidatorSet, error)
}

type MembershipFile struct {
	FileName string
}

func NewMembershipFile(fileName string) *MembershipFile {
	return &MembershipFile{
		FileName: fileName,
	}
}

// GetValidators gets the membership config from a file.
func (mf MembershipFile) GetValidators() (*ValidatorSet, error) {
	return GetValidatorsFromFile(mf.FileName)
}

type MembershipString string

// GetValidators gets the membership config from the input string.
func (s MembershipString) GetValidators() (*ValidatorSet, error) {
	return GetValidatorsFromStr(string(s))
}

type MembershipEnv string

// GetValidators gets the membership config from the input environment variable.
func (e MembershipEnv) GetValidators() (*ValidatorSet, error) {
	return GetValidatorsFromEnv(string(e))
}
