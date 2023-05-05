package generator

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"text/template"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	addr "github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/e2e/internal/fs"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
)

type Validator struct {
	N int

	Addr    addr.Address
	NetAddr string
	Weight  *abi.TokenAmount

	APIPort       string
	P2PPort       string
	IPAddr        string
	LibP2PKey     crypto.PrivKey
	WalletPrivKey *types.KeyInfo
	LibP2PTCPAddr multiaddr.Multiaddr
	LibP2PUDPAddr multiaddr.Multiaddr

	Set *ValidatorSet
}

type ValidatorData struct {
	Addr    addr.Address     `json:"addr"`
	NetAddr string           `json:"net_addr"`
	Weight  *abi.TokenAmount `json:"weight"`
}

type ValidatorSetData struct {
	Validators          []*ValidatorData `json:"validators"`
	ConfigurationNumber int              `json:"configuration_number"`
}

func NewValidator(n int, ip string, ports ...string) (*Validator, error) {
	w := abi.NewTokenAmount(0)
	v := Validator{
		N:       n,
		APIPort: fmt.Sprintf("123%d", n),
		P2PPort: fmt.Sprintf("400%d", n),
		IPAddr:  fmt.Sprintf("192.168.10.%d", n+2),
		Weight:  &w,
	}
	err := v.newWalletKey()
	if err != nil {
		return nil, err
	}
	err = v.newLibP2P(ip, ports...)
	if err != nil {
		return nil, err
	}

	peerID, err := peer.IDFromPrivateKey(v.LibP2PKey)
	if err != nil {
		return nil, err
	}

	info := peer.AddrInfo{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{v.LibP2PTCPAddr},
	}
	ddd, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return nil, err
	}
	v.NetAddr = ddd[0].String()

	return &v, nil
}

func (v *Validator) Data() *ValidatorData {
	return &ValidatorData{
		Addr:    v.Addr,
		NetAddr: v.NetAddr,
		Weight:  v.Weight,
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

	b, err := json.Marshal(v.WalletPrivKey)
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
	b, err = crypto.MarshalPrivateKey(v.LibP2PKey)
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

	b, err = marshalMultiAddrSlice([]multiaddr.Multiaddr{v.LibP2PTCPAddr, v.LibP2PUDPAddr})
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
	setData, err := v.Set.Data()
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

	tmpl, err := template.ParseFiles("./generator/config.toml")
	// Capture any error
	if err != nil {
		return err
	}

	fi, err = os.Create(path.Join(outDir, "config.toml"))
	if err != nil {
		return err
	}
	defer func() {
		err2 := fi.Close()
		if err == nil {
			err = err2
		}
	}()

	err = tmpl.Execute(fi, v)
	if err != nil {
		return err
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
	tcp := fmt.Sprintf("1334%d", v.N)
	udp := fmt.Sprintf("1444%d", v.N)
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
	v.LibP2PKey = pk

	v.LibP2PTCPAddr, err = multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%s", ip, tcp))
	if err != nil {
		return err
	}

	v.LibP2PUDPAddr, err = multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/udp/%s/quic", ip, udp))
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

	v.Addr = kaddr
	v.WalletPrivKey = ki

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

func NewValidatorSet(startIP string, size int, nonce int, ports ...string) (*ValidatorSet, error) {
	set := ValidatorSet{
		Size:                size,
		ConfigurationNumber: nonce,
	}
	ip := startIP
	for i := 0; i < size; i++ {
		v, err := NewValidator(i, ip)
		if err != nil {
			return nil, err
		}
		ip = nextIP(ip)
		set.Validators = append(set.Validators, v)
	}
	for _, v := range set.Validators {
		v.Set = &set
	}
	return &set, nil
}

func (s *ValidatorSet) StoreNet(outDir string) error {
	fi, err := os.Create(path.Join(outDir, "mir.net"))
	if err != nil {
		return err
	}
	defer func() {
		err2 := fi.Close()
		if err == nil {
			err = err2
		}
	}()

	for _, v := range s.Validators {
		_, err := fi.Write([]byte(v.NetAddr + "\n"))
		if err != nil {
			return err
		}

	}
	return nil
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

func SaveNewNetworkConfig(size int, firstIP string, nonce int, outputDir, genesisDir string) error {
	validatorSet, err := NewValidatorSet(firstIP, size, nonce)
	if err != nil {
		return err
	}

	// --- save node configs

	for i, v := range validatorSet.Validators {
		nodeConfigDir := path.Join(outputDir, fmt.Sprintf("node%d", i))

		err = v.SaveToFile(nodeConfigDir)
		if err != nil {
			return err
		}
	}

	// --- save genesis.car

	r, err := fs.FindRoot()
	if err != nil {
		panic(err)
	}
	ScriptPath, err := filepath.Abs(filepath.Join(r, "scripts", "mir"))
	if err != nil {
		panic(err)
	}
	err = fs.CopyFile(path.Join(ScriptPath, "genesis.car"), path.Join(genesisDir, "genesis.car"))
	if err != nil {
		panic(err)
	}

	return nil
}
