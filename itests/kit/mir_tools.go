package kit

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/exitcode"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

const (
	finalityTimeout  = 1200
	balanceSleepTime = 3
)

// SubnetHeightCheckForBlocks checks that `n` blocks with correct heights in the subnet will be mined.
// TODO: Ideally we should wait for a specific epoch/height of a block to see that all nodes are
// able to sync and mine up till there.
// A way of doing it may be to add a ChainHead listener that blocks until the height is reached
// in an independent go routine for each node
func SubnetHeightCheckForBlocks(ctx context.Context, n int, api lapi.FullNode) error {
	heads, err := api.ChainNotify(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("closed channel")
	case <-heads:
	}

	currHead, err := api.ChainHead(ctx)
	if err != nil {
		return err
	}

	i := 0
	for i < n {
		select {
		case <-ctx.Done():
			return fmt.Errorf("closed channel")
		case <-heads:
			newHead, err := api.ChainHead(ctx)
			if err != nil {
				return err
			}

			if newHead.Height() <= currHead.Height() {
				return fmt.Errorf("wrong %d block height: prev block height - %d, current head height - %d",
					i, currHead.Height(), newHead.Height())
			}

			currHead = newHead
			i++
		}
	}

	return nil
}

func MirNodesWaitMsg(ctx context.Context, msg cid.Cid, nodes ...*TestFullNode) error {
	for _, node := range nodes {
		res, err := node.StateWaitMsg(ctx, msg, 1, lapi.LookbackNoLimit, true)
		if err != nil {
			return err
		}
		if res.Receipt.ExitCode != exitcode.Ok {
			return fmt.Errorf("wrong exit code: %v", res.Receipt.ExitCode)
		}
	}
	return nil
}

func WaitForBalance(ctx context.Context, addr addr.Address, balance uint64, api lapi.FullNode) error {
	currentBalance, err := api.WalletBalance(ctx, addr)
	if err != nil {
		return err
	}
	targetBalance := types.FromFil(balance)
	ticker := time.NewTicker(balanceSleepTime * time.Second)
	defer ticker.Stop()

	timer := time.After(finalityTimeout * time.Second)

	for big.Cmp(currentBalance, targetBalance) != 1 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("closed channel")
		case <-ticker.C:
			currentBalance, err = api.WalletBalance(ctx, addr)
			if err != nil {
				return err
			}
		case <-timer:
			return fmt.Errorf("balance timer exceeded")
		}
	}

	return nil
}

func GetFreeTCPLocalAddr() (addr string, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close() // nolint
			return fmt.Sprintf("127.0.0.1:%d", l.Addr().(*net.TCPAddr).Port), nil
		}
	}
	return
}

func GetFreeLibp2pLocalAddr() (m multiaddr.Multiaddr, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close() // nolint
			return multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", l.Addr().(*net.TCPAddr).Port))
		}
	}
	return
}

func GetLibp2pAddr(privKey []byte) (m multiaddr.Multiaddr, err error) {
	saddr, err := GetFreeLibp2pLocalAddr()
	if err != nil {
		return nil, err
	}

	priv, err := crypto.UnmarshalPrivateKey(privKey)
	if err != nil {
		return nil, err
	}

	peerID, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		panic(err)
	}

	peerInfo := peer.AddrInfo{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{saddr},
	}

	addrs, err := peer.AddrInfoToP2pAddrs(&peerInfo)
	if err != nil {
		return nil, err
	}

	return addrs[0], nil
}

func NodeLibp2pAddr(h host.Host) (m multiaddr.Multiaddr, err error) {
	peerInfo := peer.AddrInfo{
		ID:    h.ID(),
		Addrs: []multiaddr.Multiaddr{h.Addrs()[0]},
	}

	addrs, err := peer.AddrInfoToP2pAddrs(&peerInfo)
	if err != nil {
		return nil, err
	}

	return addrs[0], nil
}

func Delay(sec int) {
	rand.Seed(time.Now().UnixNano())
	n := rand.Intn(sec)
	time.Sleep(time.Duration(n) * time.Second)
}
