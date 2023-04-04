package kit

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/lotus/chain/consensus/mir/db"
	"github.com/filecoin-project/lotus/chain/types"
)

type TestValidator struct {
	TestMiner

	mirValidator *MirValidator
	mirPrivKey   crypto.PrivKey
	mirHost      host.Host
	mirAddr      address.Address
	mirMultiAddr []multiaddr.Multiaddr
}

func NewTestValidator(t *testing.T, full *TestFullNode, miner TestMiner) *TestValidator {
	addr, err := full.WalletNew(context.Background(), types.KTSecp256k1)
	require.NoError(t, err)

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	require.NoError(t, err)

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.DefaultTransports,
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	require.NoError(t, err)

	return &TestValidator{
		TestMiner:    miner,
		mirPrivKey:   priv,
		mirHost:      h,
		mirAddr:      addr,
		mirMultiAddr: h.Addrs(),
	}
}

func (tv *TestValidator) GetMirID() string {
	return tv.mirAddr.String()
}

func (tv *TestValidator) GetRawDB() map[datastore.Key][]byte {
	return tv.mirValidator.db.db
}

func (tv *TestValidator) GetDB() db.DB {
	return tv.mirValidator.db
}
