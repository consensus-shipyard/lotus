package fifo

import (
	"testing"

	"github.com/ipfs/go-cid"
	u "github.com/ipfs/go-ipfs-util"
	"github.com/stretchr/testify/require"

	mirproto "github.com/filecoin-project/mir/pkg/pb/trantorpb/types"
)

func TestMirFIFOPool(t *testing.T) {
	p := New()

	c1 := cid.NewCidV0(u.Hash([]byte("req1")))
	c2 := cid.NewCidV0(u.Hash([]byte("req2")))

	inProgress := p.AddTx(c1, &mirproto.Transaction{
		ClientId: "client1", Data: []byte{},
	})
	require.Equal(t, false, inProgress)

	inProgress = p.AddTx(c1, &mirproto.Transaction{
		ClientId: "client1", Data: []byte{},
	})
	require.Equal(t, true, inProgress)

	inProgress = p.DeleteTx(c1, 0)
	require.Equal(t, true, inProgress)

	inProgress = p.DeleteTx(c2, 0)
	require.Equal(t, false, inProgress)
}
