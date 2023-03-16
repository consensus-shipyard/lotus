package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
)

type Validator struct {
	libp2pKey     crypto.PrivKey
	walletPrivKey *types.KeyInfo
	walletPubKey  addr.Address
	libp2pTCPAddr multiaddr.Multiaddr
	libp2pUDPAddr multiaddr.Multiaddr
	netAddr       string
	set           *ValidatorSet
}

type ValidatorData struct {
	Addr    addr.Address `json:"addr"`
	NetAddr string       `json:"net_addr"`
}

type ValidatorSetData struct {
	Validators          []*ValidatorData `json:"validators"`
	ConfigurationNumber int              `json:"configuration_number"`
}

func NewValidator(ip string, ports ...string) (*Validator, error) {
	v := Validator{}
	err := v.newWalletKey()
	if err != nil {
		return nil, err
	}
	err = v.newLibP2P(ip, ports...)
	if err != nil {
		return nil, err
	}

	peerID, err := peer.IDFromPrivateKey(v.libp2pKey)
	if err != nil {
		return nil, err
	}

	info := peer.AddrInfo{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{v.libp2pTCPAddr},
	}
	ddd, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return nil, err
	}
	v.netAddr = ddd[0].String()
	return &v, nil
}

func (v *Validator) Data() *ValidatorData {
	return &ValidatorData{
		Addr:    v.walletPubKey,
		NetAddr: v.netAddr,
	}
}

func (v *Validator) SaveToFile(outDir string) error {
	err := os.MkdirAll(outDir, os.ModePerm)
	if err != nil {
		return err
	}
	fi, err := os.Create(path.Join(outDir, "wallet.key"))
	if err != nil {
		return err
	}
	defer func() {
		err2 := fi.Close()
		if err == nil {
			err = err2
		}
	}()

	b, err := json.Marshal(v.walletPrivKey)
	if err != nil {
		return err
	}

	if _, err := fi.Write(b); err != nil {
		return fmt.Errorf("failed to write key info to file: %w", err)
	}

	file, err := os.Create(path.Join(outDir, "mir.key"))
	if err != nil {
		return fmt.Errorf("error creating libp2p key: %w", err)
	}
	b, err = crypto.MarshalPrivateKey(v.libp2pKey)
	if err != nil {
		return fmt.Errorf("error marshalling libp2p key: %w", err)
	}
	_, err = file.Write(b)
	if err != nil {
		return fmt.Errorf("error writing libp2p key in file: %w", err)
	}

	file, err = os.Create(path.Join(outDir, "mir.maddr"))
	if err != nil {
		return fmt.Errorf("error creating libp2p multiaddr: %w", err)
	}

	b, err = marshalMultiAddrSlice([]multiaddr.Multiaddr{v.libp2pTCPAddr, v.libp2pUDPAddr})
	if err != nil {
		return err
	}
	_, err = file.Write(b)
	if err != nil {
		return fmt.Errorf("error writing libp2p multiaddr in file: %w", err)
	}

	// ----

	fi, err = os.Create(path.Join(outDir, "mir.validators"))
	if err != nil {
		return err
	}
	defer func() {
		err2 := fi.Close()
		if err == nil {
			err = err2
		}
	}()
	setData, err := v.set.Data()
	if err != nil {
		return err
	}
	b, err = json.MarshalIndent(setData, "", "    ")
	if err != nil {
		return err
	}

	_, err = fi.Write(b)
	if err != nil {
		return fmt.Errorf("error writing validators in file: %w", err)
	}

	return nil
}

func marshalMultiAddrSlice(ma []multiaddr.Multiaddr) ([]byte, error) {
	var out []string
	for _, a := range ma {
		out = append(out, a.String())
	}
	return json.Marshal(&out)
}

func (v *Validator) newLibP2P(ip string, ports ...string) error {
	tcp := "1347"
	udp := "1348"
	if len(ports) == 2 {
		tcp = ports[0]
		udp = ports[1]
	} else if len(ports) == 1 {
		tcp = ports[0]
	}
	pk, err := genLibp2pKey()
	if err != nil {
		return fmt.Errorf("error generating libp2p key: %w", err)
	}
	v.libp2pKey = pk

	v.libp2pTCPAddr, err = multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%s", ip, tcp))
	if err != nil {
		return err
	}

	v.libp2pUDPAddr, err = multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/udp/%s", ip, udp))
	if err != nil {
		return err
	}
	return nil
}

func genLibp2pKey() (crypto.PrivKey, error) {
	pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}
	return pk, nil
}

func (v *Validator) newWalletKey() error {
	memks := wallet.NewMemKeyStore()
	w, err := wallet.NewWallet(memks)
	if err != nil {
		return err
	}

	ctx := context.Background()

	kaddr, err := w.WalletNew(ctx, types.KTSecp256k1)
	if err != nil {
		return err
	}

	ki, err := w.WalletExport(ctx, kaddr)
	if err != nil {
		return err
	}

	v.walletPubKey = kaddr
	v.walletPrivKey = ki

	return nil
}

func (v *ValidatorSet) Data() (*ValidatorSetData, error) {
	data := ValidatorSetData{
		Validators:          []*ValidatorData{},
		ConfigurationNumber: v.ConfigurationNumber,
	}

	for _, val := range v.Validators {
		data.Validators = append(data.Validators, val.Data())
	}

	return &data, nil
}

type ValidatorSet struct {
	Size                int
	Validators          []*Validator
	ConfigurationNumber int
}

func NewValidatorSet(startIP string, size int, nonce int) (*ValidatorSet, error) {
	set := ValidatorSet{
		Size:                size,
		ConfigurationNumber: nonce,
	}
	ip := startIP
	for i := 0; i < size; i++ {
		v, err := NewValidator(ip)
		if err != nil {
			return nil, err
		}
		ip = nextIP(ip)
		set.Validators = append(set.Validators, v)
	}
	for _, v := range set.Validators {
		v.set = &set
	}
	return &set, nil
}

func nextIP(ip string) string {
	if ip == "127.0.0.1" {
		return ip
	}

	ipAddr := net.ParseIP(ip)
	ipv4Addr := ipAddr.To4()

	ipv4Addr[3]++

	ipAddr = net.IPv4(ipv4Addr[0], ipv4Addr[1], ipv4Addr[2], ipv4Addr[3])

	return ipAddr.String()
}

func main() {
	firstIP := flag.String("ip", "127.0.0.1", "IP address of the first validator node")
	n := flag.Int("n", 4, "amount of node validators")
	nonce := flag.Int("nonce", 0, "configuration number")
	outputDir := flag.String("output", "./tmmmmmm", "output directory")

	validatorSet, err := NewValidatorSet(*firstIP, *n, *nonce)
	if err != nil {
		panic(err)
	}

	fmt.Println(validatorSet)

	for i, v := range validatorSet.Validators {
		nodeConfigDir := path.Join(*outputDir, fmt.Sprintf("node%d", i))

		err = v.SaveToFile(nodeConfigDir)
		if err != nil {
			panic(err)
		}
	}
}
