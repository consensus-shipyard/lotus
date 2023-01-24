package validator

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ipfs/go-cid"
	u "github.com/ipfs/go-ipfs-util"

	"github.com/filecoin-project/go-address"
)

type ValidatorSet struct {
	ConfigurationNumber uint64      `json:"configuration_number"`
	Validators          []Validator `json:"validators"`
}

func NewValidatorSet(n uint64, vals []Validator) *ValidatorSet {
	return &ValidatorSet{
		ConfigurationNumber: n,
		Validators:          vals,
	}
}

func (set *ValidatorSet) Size() int {
	return len(set.Validators)
}

func (set *ValidatorSet) JSONString() string {
	b, err := json.Marshal(set)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (set *ValidatorSet) GetConfigurationNumber() uint64 {
	return set.ConfigurationNumber
}

func (set *ValidatorSet) Equal(o *ValidatorSet) bool {
	if set.ConfigurationNumber != o.ConfigurationNumber {
		return false
	}
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
	var hs [][]byte

	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, set.ConfigurationNumber)
	hs = append(hs, b)

	for _, v := range set.Validators {
		b, err := v.Bytes()
		if err != nil {
			return nil, err
		}
		hs = append(hs, b)
	}
	SortByteSlices(hs)

	return cid.NewCidV0(u.Hash(bytes.Join(hs, nil))).Bytes(), nil
}

func (set *ValidatorSet) GetValidators() []Validator {
	return set.Validators
}

func (set *ValidatorSet) GetValidatorIDs() (ids []address.Address) {
	for _, v := range set.Validators {
		ids = append(ids, v.Addr)
	}
	return
}

func (set *ValidatorSet) HasValidatorWithID(id string) bool {
	for _, v := range set.Validators {
		if v.ID() == id {
			return true
		}
	}
	return false
}

// Save saves the validator set to the file in JSON format.
func (set *ValidatorSet) Save(path string) error {
	var err error

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	defer func() {
		cerr := f.Close()
		if err == nil {
			err = fmt.Errorf("error closing membership config file: %w", cerr)
		}
	}()

	b, err := json.Marshal(set)
	if err != nil {
		return err
	}

	_, err = f.Write(b)
	return err
}

// NewValidatorsFromEnv initializes a validator set based on the data from the environment variable.
func NewValidatorsFromEnv(env string) (*ValidatorSet, error) {
	e := os.Getenv(env)
	if e == "" {
		return nil, fmt.Errorf("empty validator string")
	}

	return parseValidatorString(e)
}

func parseValidatorString(s string) (*ValidatorSet, error) {
	var validators []Validator
	r := strings.Split(s, ";")

	if len(r) != 2 {
		return nil, fmt.Errorf("incorrect validator string %s", s)
	}

	n, err := strconv.ParseUint(r[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configuration number: %w", err)
	}

	for _, next := range SplitAndTrimEmpty(r[1], ",", " ") {
		v, err := NewValidatorFromString(next)
		if err != nil {
			return nil, err
		}

		validators = append(validators, v)
	}

	return NewValidatorSet(n, validators), nil
}

// NewValidatorSetFromString reads a validator set from the string.
func NewValidatorSetFromString(s string) (*ValidatorSet, error) {
	return parseValidatorString(s)
}

// NewValidatorSetFromValidators creates a validator set from the validators.
func NewValidatorSetFromValidators(n uint64, vs ...Validator) *ValidatorSet {
	return &ValidatorSet{
		ConfigurationNumber: n,
		Validators:          vs[:],
	}
}

func loadValidatorSetFromFile(path string) (*ValidatorSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	var v ValidatorSet
	err = json.Unmarshal(b, &v)
	if err != nil {
		return nil, fmt.Errorf("failed to read validator set from %v: %w", path, err)
	}

	return &v, nil
}

// NewValidatorSetFromFile reads a validator set based from the file.
func NewValidatorSetFromFile(path string) (*ValidatorSet, error) {
	return loadValidatorSetFromFile(path)
}

func SortByteSlices(src [][]byte) {
	sort.Slice(src, func(i, j int) bool { return bytes.Compare(src[i], src[j]) < 0 })
}
