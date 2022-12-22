package validator

import (
	"bufio"
	"errors"
	"fmt"
	"os"

	"github.com/ipfs/go-cid"
	u "github.com/ipfs/go-ipfs-util"
)

type ValidatorSet struct {
	Validators []Validator
}

func NewValidatorSet(vals []Validator) *ValidatorSet {
	return &ValidatorSet{Validators: vals}
}

func (set *ValidatorSet) Size() int {
	return len(set.Validators)
}

func (set *ValidatorSet) Equal(o *ValidatorSet) bool {
	if set == nil && o == nil {
		return true
	}
	if set == nil || o == nil {
		return true
	}
	if set.Size() != o.Size() {
		return false
	}
	for i, v := range set.Validators {
		if v != o.Validators[i] {
			return false
		}
	}
	return true
}

func (set *ValidatorSet) Hash() ([]byte, error) {
	var hs []byte
	for _, v := range set.Validators {
		b, err := v.Bytes()
		if err != nil {
			return nil, err
		}
		hs = append(hs, b...)
	}
	return cid.NewCidV0(u.Hash(hs)).Bytes(), nil
}

func (set *ValidatorSet) GetValidators() []Validator {
	return set.Validators
}

func (set *ValidatorSet) HasValidatorWithID(id string) bool {
	for _, v := range set.Validators {
		if v.ID() == id {
			return true
		}
	}
	return false
}

// StoreToFile stores validators to the file.
func (set *ValidatorSet) StoreToFile(name string) error {
	var err error

	f, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}

	defer func() {
		cerr := f.Close()
		if err == nil {
			err = fmt.Errorf("error closing membership config file: %w", cerr)
		}
	}()

	for _, v := range set.Validators {
		if _, err = f.WriteString(v.String() + "\n"); err != nil {
			return err
		}
	}
	return err
}

// NewValidatorsFromEnv initializes a validator set based on the data from the environment variable.
func NewValidatorsFromEnv(env string) (*ValidatorSet, error) {
	var validators []Validator
	input := os.Getenv(env)
	if input == "" {
		return nil, fmt.Errorf("empty validator string")
	}

	for _, next := range SplitAndTrimEmpty(input, ",", " ") {
		v, err := NewValidatorFromString(next)
		if err != nil {
			return nil, err
		}

		validators = append(validators, v)
	}

	return NewValidatorSet(validators), nil
}

// NewValidatorsFromString initializes a validator set based on the data from the string.
func NewValidatorsFromString(input string) (*ValidatorSet, error) {
	var validators []Validator
	if input == "" {
		return nil, fmt.Errorf("empty validator string")
	}

	for _, next := range SplitAndTrimEmpty(input, ",", " ") {
		v, err := NewValidatorFromString(next)
		if err != nil {
			return nil, err
		}

		validators = append(validators, v)
	}

	return NewValidatorSet(validators), nil
}

// NewValidatorSetFromFile initializes a validator set based on the data from the file.
func NewValidatorSetFromFile(path string) (*ValidatorSet, error) {
	var validators []Validator
	var err error

	_, err = os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no membership config found in path: %s", path)
	}
	readFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		cerr := readFile.Close()
		if err == nil {
			err = fmt.Errorf("error closing membership config file: %w", cerr)
		}
	}()

	fileScanner := bufio.NewScanner(readFile)
	fileScanner.Split(bufio.ScanLines)

	for fileScanner.Scan() {
		v, err := NewValidatorFromString(fileScanner.Text())
		if err != nil {
			return nil, err
		}
		validators = append(validators, v)
	}
	return NewValidatorSet(validators), err
}
