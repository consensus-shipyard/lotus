package validator

import (
	"fmt"
	"os"
	"strings"

	"github.com/multiformats/go-multiaddr"
	"go.uber.org/zap/buffer"

	addr "github.com/filecoin-project/go-address"
	t "github.com/filecoin-project/mir/pkg/types"
)

type Validator struct {
	Addr addr.Address
	// FIXME: Consider using a multiaddr
	NetAddr string
}

// NewValidatorFromString parses a validator address from the string.
// OpaqueNetAddr can contain GRPC or libp2p addresses.
//
// Examples of validator strings:
//   - t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@/ip4/127.0.0.1/tcp/10000/p2p/12D3KooWJhKBXvytYgPCAaiRtiNLJNSFG5jreKDu2jiVpJetzvVJ
//   - t1wpixt5mihkj75lfhrnaa6v56n27epvlgwparujy@127.0.0.1:1000
//
// FIXME: Consider using json serde for this to support multiple multiaddr for validators.
func NewValidatorFromString(input string) (Validator, error) {
	parts := strings.Split(input, "@")
	if len(parts) != 2 {
		return Validator{}, fmt.Errorf("failed to parse validators string")
	}
	ID := parts[0]
	opaqueNetAddr := parts[1]

	a, err := addr.NewFromString(ID)
	if err != nil {
		return Validator{}, err
	}
	ma, err := multiaddr.NewMultiaddr(opaqueNetAddr)
	if err != nil {
		return Validator{}, err
	}

	return Validator{
		Addr:    a,
		NetAddr: ma.String(),
	}, nil
}

func (v *Validator) ID() string {
	return v.Addr.String()
}

func (v *Validator) Bytes() ([]byte, error) {
	var b buffer.Buffer
	if err := v.MarshalCBOR(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (v *Validator) String() string {
	return fmt.Sprintf("%s@%s", v.Addr.String(), v.NetAddr)
}

// AppendToFile appends the validator to the file.
func (v *Validator) AppendToFile(name string) error {
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

	if _, err = f.WriteString(v.String() + "\n"); err != nil {
		return err
	}

	return err
}

func SplitAndTrimEmpty(s, sep, cutset string) []string {
	if s == "" {
		return []string{}
	}

	spl := strings.Split(s, sep)
	nonEmptyStrings := make([]string, 0, len(spl))

	for i := 0; i < len(spl); i++ {
		element := strings.Trim(spl[i], cutset)
		if element != "" {
			nonEmptyStrings = append(nonEmptyStrings, element)
		}
	}

	return nonEmptyStrings
}

// Membership validates that validators addresses are correct multi-addresses and
// returns all the corresponding IDs and map between these IDs and the multi-addresses.
func Membership(validators []Validator) ([]t.NodeID, map[t.NodeID]t.NodeAddress, error) {
	var nodeIDs []t.NodeID
	nodeAddrs := make(map[t.NodeID]t.NodeAddress)

	for _, v := range validators {
		id := t.NodeID(v.ID())
		a, err := multiaddr.NewMultiaddr(v.NetAddr)
		if err != nil {
			return nil, nil, err
		}
		nodeIDs = append(nodeIDs, id)
		nodeAddrs[id] = a
	}

	return nodeIDs, nodeAddrs, nil
}
