package kit

import (
	"context"
	"fmt"
	"testing"

	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multiaddr"

	"github.com/filecoin-project/go-address"
	mirlibp2pnet "github.com/filecoin-project/mir/pkg/net"
	mirlibp2p "github.com/filecoin-project/mir/pkg/net/libp2p"
	mirtypes "github.com/filecoin-project/mir/pkg/types"

	"github.com/filecoin-project/lotus/chain/consensus/mir"
	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/consensus/mir/membership"
)

type MirConfig struct {
	Delay              int
	MembershipFileName string
	MembershipString   string
	MembershipType     string
	MembershipFilename string
	Databases          map[string]*TestDB
	MockedTransport    bool
}

func DefaultMirConfig() *MirConfig {
	return &MirConfig{
		MembershipType: membership.StringSource,
	}
}

type MirValidator struct {
	t     *testing.T
	miner *TestValidator

	privKey          crypto.PrivKey
	host             host.Host
	addr             address.Address
	multiAddr        []multiaddr.Multiaddr
	mockedNet        *MockedTransport
	net              mirlibp2pnet.Transport
	stop             context.CancelFunc
	db               *TestDB
	membershipString string
	membership       membership.Reader
	config           *MirConfig
}

func NewMirValidator(t *testing.T, miner *TestValidator, db *TestDB, cfg *MirConfig) (*MirValidator, error) {
	v := MirValidator{
		t:         t,
		miner:     miner,
		privKey:   miner.mirPrivKey,
		host:      miner.mirHost,
		addr:      miner.mirAddr,
		multiAddr: miner.mirMultiAddr,
		db:        db,
		config:    cfg,
	}

	switch cfg.MembershipType {
	case membership.FakeSource:
		v.membership = fakeMembership{}
	case membership.StringSource:
		if cfg.MembershipString == "" {
			return nil, fmt.Errorf("empty membership string")
		}
		v.membershipString = cfg.MembershipString
		v.membership = membership.StringMembership(cfg.MembershipString)
	case membership.FileSource:
		if cfg.MembershipFileName == "" {
			return nil, fmt.Errorf("membership file is not specified")
		}
		v.membership = membership.FileMembership{FileName: cfg.MembershipFileName}
	case membership.OnChainSource:
		cl := NewStubJSONRPCClient()
		cl.nextSet = cfg.MembershipString
		v.membership = membership.NewOnChainMembershipClient(cl, ITestSubnet)
	default:
		return nil, fmt.Errorf("unknown membership type")
	}

	var netLogger = mir.NewLogger(v.addr.String())
	if cfg.MockedTransport {
		v.mockedNet = NewTransport(mirlibp2p.DefaultParams(), mirtypes.NodeID(v.addr.String()), v.host, netLogger)
		v.net = v.mockedNet
	} else {
		v.net = mirlibp2p.NewTransport(mirlibp2p.DefaultParams(), mirtypes.NodeID(v.addr.String()), v.host, netLogger)
	}

	return &v, nil
}

func (v *MirValidator) MineBlocks(ctx context.Context) error {
	cfg := mir.Config{
		BaseConfig: &mir.BaseConfig{
			Addr:      v.addr,
			GroupName: v.t.Name(),
		},
		Consensus: mir.DefaultConsensusConfig(),
	}

	ctx, cancel := context.WithCancel(ctx)
	v.stop = cancel

	return mir.Mine(ctx, v.net, v.miner.FullNode, v.db, v.membership, &cfg)
}

func (v *MirValidator) GetRawDB() map[datastore.Key][]byte {
	return v.db.db
}

func (v *MirValidator) GetDB() db.DB {
	return v.db
}
