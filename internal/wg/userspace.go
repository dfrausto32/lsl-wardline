package wg

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// Userspace runs WireGuard inside this process using wireguard-go. It needs a
// TUN device but not the kernel module, so it works in any container.
type Userspace struct {
	mu   sync.Mutex
	devs map[string]*device.Device
}

func NewUserspace() *Userspace { return &Userspace{devs: map[string]*device.Device{}} }

func keyHex(b64 string) (string, error) {
	raw, err := decodeKey(b64)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (u *Userspace) Configure(_ context.Context, name string, cfg InterfaceConfig) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	priv, err := keyHex(cfg.PrivateKey)
	if err != nil {
		return err
	}
	dev, exists := u.devs[name]
	if !exists {
		t, err := tun.CreateTUN(name, device.DefaultMTU)
		if err != nil {
			return fmt.Errorf("create tunnel device: %w", err)
		}
		logger := device.NewLogger(device.LogLevelError, "wireguard-go "+name+": ")
		dev = device.NewDevice(t, conn.NewDefaultBind(), logger)
		u.devs[name] = dev
	}
	uapi := "private_key=" + priv + "\nlisten_port=" + strconv.Itoa(cfg.ListenPort) + "\n"
	if err := dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("set interface: %w", err)
	}
	if err := dev.Up(); err != nil {
		return err
	}
	if cfg.Address.IsValid() {
		if err := setAddress(name, cfg.Address); err != nil {
			return err
		}
	}
	return nil
}

func (u *Userspace) device(name string) (*device.Device, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	d, ok := u.devs[name]
	if !ok {
		return nil, fmt.Errorf("interface %s does not exist", name)
	}
	return d, nil
}

func (u *Userspace) Peers(_ context.Context, name string) ([]PeerState, error) {
	dev, err := u.device(name)
	if err != nil {
		return nil, err
	}
	dump, err := dev.IpcGet()
	if err != nil {
		return nil, err
	}
	return parseUAPI(dump)
}

// parseUAPI reads the text format wireguard-go returns from IpcGet.
func parseUAPI(dump string) ([]PeerState, error) {
	var out []PeerState
	var cur *PeerState
	var hsSec, hsNsec int64
	flush := func() {
		if cur == nil {
			return
		}
		if hsSec != 0 || hsNsec != 0 {
			cur.LastHandshake = time.Unix(hsSec, hsNsec)
		}
		cur.AllowedIPs = sortedIPs(cur.AllowedIPs)
		out = append(out, *cur)
		cur, hsSec, hsNsec = nil, 0, 0
	}
	for _, line := range strings.Split(dump, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if k == "public_key" {
			flush()
			raw, err := hex.DecodeString(v)
			if err != nil {
				return nil, err
			}
			cur = &PeerState{}
			cur.PublicKey = base64.StdEncoding.EncodeToString(raw)
			continue
		}
		if cur == nil {
			continue // interface-level line
		}
		switch k {
		case "preshared_key":
			raw, err := hex.DecodeString(v)
			if err != nil {
				return nil, err
			}
			if strings.Trim(v, "0") != "" {
				cur.PresharedKey = base64.StdEncoding.EncodeToString(raw)
			}
		case "endpoint":
			cur.Endpoint, cur.LastEndpoint = v, v
		case "last_handshake_time_sec":
			hsSec, _ = strconv.ParseInt(v, 10, 64)
		case "last_handshake_time_nsec":
			hsNsec, _ = strconv.ParseInt(v, 10, 64)
		case "tx_bytes":
			cur.TxBytes, _ = strconv.ParseInt(v, 10, 64)
		case "rx_bytes":
			cur.RxBytes, _ = strconv.ParseInt(v, 10, 64)
		case "persistent_keepalive_interval":
			cur.Keepalive, _ = strconv.Atoi(v)
		case "allowed_ip":
			p, err := netip.ParsePrefix(v)
			if err != nil {
				return nil, err
			}
			cur.AllowedIPs = append(cur.AllowedIPs, p)
		}
	}
	flush()
	sortPeers(out)
	return out, nil
}

func sortPeers(p []PeerState) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j].PublicKey < p[j-1].PublicKey; j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

func uapiPeer(p PeerConfig) (string, error) {
	pub, err := keyHex(p.PublicKey)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("public_key=" + pub + "\n")
	psk := strings.Repeat("0", 64) // all zeros clears the preshared key
	if p.PresharedKey != "" {
		if psk, err = keyHex(p.PresharedKey); err != nil {
			return "", err
		}
	}
	sb.WriteString("preshared_key=" + psk + "\n")
	sb.WriteString("replace_allowed_ips=true\n")
	for _, ip := range sortedIPs(p.AllowedIPs) {
		sb.WriteString("allowed_ip=" + ip.String() + "\n")
	}
	sb.WriteString("persistent_keepalive_interval=" + strconv.Itoa(p.Keepalive) + "\n")
	if p.Endpoint != "" {
		sb.WriteString("endpoint=" + p.Endpoint + "\n")
	}
	return sb.String(), nil
}

func (u *Userspace) ApplyDiff(_ context.Context, name string, d Diff) error {
	dev, err := u.device(name)
	if err != nil {
		return err
	}
	var sb strings.Builder
	for _, p := range append(append([]PeerConfig{}, d.Add...), d.Update...) {
		s, err := uapiPeer(p)
		if err != nil {
			return err
		}
		sb.WriteString(s)
	}
	for _, k := range d.Remove {
		pub, err := keyHex(k)
		if err != nil {
			return err
		}
		sb.WriteString("public_key=" + pub + "\nremove=true\n")
	}
	if sb.Len() == 0 {
		return nil
	}
	return dev.IpcSet(sb.String())
}

func (u *Userspace) Remove(_ context.Context, name string) error {
	u.mu.Lock()
	dev, ok := u.devs[name]
	delete(u.devs, name)
	u.mu.Unlock()
	if ok {
		dev.Close()
	}
	return nil
}
