package validator

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ipfs/go-cid"
	u "github.com/ipfs/go-ipfs-util"

	"github.com/filecoin-project/go-address"
)

type Reader interface {
	GetValidatorSet() (*Set, error)
}

type Set struct {
	ConfigurationNumber uint64      `json:"configuration_number"`
	Validators          []Validator `json:"validators"`
}

// NewValidatorSetFromFile reads a validator set based from the file.
func NewValidatorSetFromFile(path string) (*Set, error) {
	return loadValidatorSetFromFile(path)
}

// NewValidatorSetFromString reads a validator set from the string.
func NewValidatorSetFromString(s string) (*Set, error) {
	return parseValidatorString(s)
}

// NewValidatorSetFromValidators creates a validator set from the validators.
func NewValidatorSetFromValidators(n uint64, vs ...Validator) *Set {
	return &Set{
		ConfigurationNumber: n,
		Validators:          vs[:],
	}
}

// NewValidatorSetFromEnv initializes a validator set based on the data from the environment variable.
func NewValidatorSetFromEnv(env string) (*Set, error) {
	e := os.Getenv(env)
	if e == "" {
		return nil, fmt.Errorf("empty validator string")
	}

	return parseValidatorString(e)
}

func NewValidatorSet(n uint64, vals []Validator) *Set {
	return &Set{
		ConfigurationNumber: n,
		Validators:          vals,
	}
}

func (s *Set) Size() int {
	return len(s.Validators)
}

func (s *Set) JSONString() string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (s *Set) GetConfigurationNumber() uint64 {
	return s.ConfigurationNumber
}

func (s *Set) Equal(o *Set) bool {
	if s.ConfigurationNumber != o.ConfigurationNumber {
		return false
	}
	if s == nil && o == nil {
		return true
	}
	if s == nil || o == nil {
		return true
	}
	if s.Size() != o.Size() {
		return false
	}
	for i, v := range s.Validators {
		if v != o.Validators[i] {
			return false
		}
	}
	return true
}

func (s *Set) Hash() ([]byte, error) {
	var hs [][]byte

	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, s.GetConfigurationNumber())
	hs = append(hs, b)

	for _, v := range s.Validators {
		b, err := v.Bytes()
		if err != nil {
			return nil, err
		}
		hs = append(hs, b)
	}
	SortByteSlices(hs)

	return cid.NewCidV0(u.Hash(bytes.Join(hs, nil))).Bytes(), nil
}

func (s *Set) GetValidators() []Validator {
	return s.Validators
}

func (s *Set) AddValidator(v Validator) {
	s.ConfigurationNumber++
	s.Validators = append(s.Validators, v)
	return
}

func (s *Set) GetValidatorIDs() (ids []address.Address) {
	for _, v := range s.Validators {
		ids = append(ids, v.Addr)
	}
	return
}

func (s *Set) HasValidatorWithID(id string) bool {
	for _, v := range s.Validators {
		if v.ID() == id {
			return true
		}
	}
	return false
}

// Save saves the validator set to the file in JSON format.
func (s *Set) Save(path string) error {
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

	b, err := json.Marshal(s)
	if err != nil {
		return err
	}

	_, err = f.Write(b)
	return err
}

// AddValidatorToFile adds a validator `v` to the membership file `path`.
// If the file does not exist it will be created.
func AddValidatorToFile(path string, v Validator) error {
	set, err := NewValidatorSetFromFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		set = NewValidatorSetFromValidators(0, v)
		return set.Save(path)
	case err != nil:
		return fmt.Errorf("error parsing membership file %s: %w", path, err)
	default:
	}
	set.AddValidator(v)
	if err := set.Save(path); err != nil {
		return fmt.Errorf("error stroing membership config: %w", err)
	}
	return nil
}

func parseValidatorString(s string) (*Set, error) {
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

func loadValidatorSetFromFile(path string) (*Set, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v Set
	err = json.Unmarshal(b, &v)
	if err != nil {
		return nil, fmt.Errorf("failed to read validator set from %v: %w", path, err)
	}

	return &v, nil
}

func SortByteSlices(src [][]byte) {
	sort.Slice(src, func(i, j int) bool { return bytes.Compare(src[i], src[j]) < 0 })
}
