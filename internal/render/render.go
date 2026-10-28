// Package render turns database state into WireGuard configuration. Configs
// are rendered, never hand-edited.
package render

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/skip2/go-qrcode"

	"lsl-wardline/internal/wg"
)

// ServerPeer is one device as the server sees it.
type ServerPeer struct {
	Label        string // shown as a comment only
	PublicKey    string
	PresharedKey string
	TunnelIP     netip.Addr
	Keepalive    int
}

// ServerInput is everything needed to render a server's config. The private
// key is the server's own and is only ever supplied on the server itself.
type ServerInput struct {
	PrivateKey string
	ListenPort int
	Address    netip.Prefix
	Peers      []ServerPeer
}

// DeviceInput is everything needed to render one device's config.
type DeviceInput struct {
	PrivateKey      string
	PresharedKey    string
	TunnelIP        netip.Addr
	NetworkRange    netip.Prefix
	ServerPublicKey string
	ServerAddress   string // a name or an IP address
	ServerPort      int
	Keepalive       int // 0 for phones
	DNS             []netip.Addr
}

var ctrl = regexp.MustCompile(`[\x00-\x1f\x7f]+`)

func comment(s string) string { return strings.TrimSpace(ctrl.ReplaceAllString(s, " ")) }

var hostOK = regexp.MustCompile(`^[A-Za-z0-9.\-:]+$`)

func checkKeys(keys ...string) error {
	for _, k := range keys {
		if k == "" {
			continue
		}
		if err := wg.ValidateKey(k); err != nil {
			return fmt.Errorf("a key is not valid")
		}
	}
	return nil
}

// ServerConfig renders the server's config in wg-quick format.
func ServerConfig(in ServerInput) (string, error) {
	if in.PrivateKey == "" {
		return "", fmt.Errorf("server key is missing")
	}
	if err := checkKeys(in.PrivateKey); err != nil {
		return "", err
	}
	if !in.Address.IsValid() {
		return "", fmt.Errorf("server address is missing")
	}
	peers := append([]ServerPeer(nil), in.Peers...)
	sort.Slice(peers, func(i, j int) bool { return peers[i].TunnelIP.Less(peers[j].TunnelIP) })

	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	fmt.Fprintf(&sb, "Address = %s\n", in.Address)
	if in.ListenPort != 0 {
		fmt.Fprintf(&sb, "ListenPort = %d\n", in.ListenPort)
	}
	fmt.Fprintf(&sb, "PrivateKey = %s\n", in.PrivateKey)
	for _, p := range peers {
		if err := checkKeys(p.PublicKey, p.PresharedKey); err != nil || p.PublicKey == "" {
			return "", fmt.Errorf("a device has an invalid key")
		}
		if !p.TunnelIP.IsValid() {
			return "", fmt.Errorf("a device has no address")
		}
		fmt.Fprintf(&sb, "\n# %s\n[Peer]\nPublicKey = %s\n", comment(p.Label), p.PublicKey)
		if p.PresharedKey != "" {
			fmt.Fprintf(&sb, "PresharedKey = %s\n", p.PresharedKey)
		}
		// Hub-and-spoke: a device is allowed exactly its own address.
		fmt.Fprintf(&sb, "AllowedIPs = %s/%d\n", p.TunnelIP, p.TunnelIP.BitLen())
		if p.Keepalive > 0 {
			fmt.Fprintf(&sb, "PersistentKeepalive = %d\n", p.Keepalive)
		}
	}
	return sb.String(), nil
}

// DesiredPeers converts server peers to the backend's desired state.
func DesiredPeers(peers []ServerPeer) []wg.PeerConfig {
	out := make([]wg.PeerConfig, 0, len(peers))
	for _, p := range peers {
		out = append(out, wg.PeerConfig{
			PublicKey:    p.PublicKey,
			PresharedKey: p.PresharedKey,
			AllowedIPs:   []netip.Prefix{netip.PrefixFrom(p.TunnelIP, p.TunnelIP.BitLen())},
			Keepalive:    p.Keepalive,
		})
	}
	return out
}

// DeviceConfig renders a device's config in the format the official WireGuard
// apps import. It contains the device's private key, so callers show it once
// and never store it.
func DeviceConfig(in DeviceInput) (string, error) {
	if err := checkKeys(in.PrivateKey, in.PresharedKey, in.ServerPublicKey); err != nil {
		return "", err
	}
	if in.PrivateKey == "" || in.ServerPublicKey == "" {
		return "", fmt.Errorf("a key is missing")
	}
	if in.ServerAddress == "" || !hostOK.MatchString(in.ServerAddress) {
		return "", fmt.Errorf("the server address is not valid")
	}
	if !in.TunnelIP.IsValid() || !in.NetworkRange.IsValid() || in.ServerPort <= 0 || in.ServerPort > 65535 {
		return "", fmt.Errorf("the device settings are incomplete")
	}
	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	fmt.Fprintf(&sb, "PrivateKey = %s\n", in.PrivateKey)
	fmt.Fprintf(&sb, "Address = %s/%d\n", in.TunnelIP, in.TunnelIP.BitLen())
	if len(in.DNS) > 0 {
		var ds []string
		for _, d := range in.DNS {
			ds = append(ds, d.String())
		}
		fmt.Fprintf(&sb, "DNS = %s\n", strings.Join(ds, ", "))
	}
	sb.WriteString("\n[Peer]\n")
	fmt.Fprintf(&sb, "PublicKey = %s\n", in.ServerPublicKey)
	if in.PresharedKey != "" {
		fmt.Fprintf(&sb, "PresharedKey = %s\n", in.PresharedKey)
	}
	fmt.Fprintf(&sb, "AllowedIPs = %s\n", in.NetworkRange.Masked())
	fmt.Fprintf(&sb, "Endpoint = %s\n", net.JoinHostPort(in.ServerAddress, strconv.Itoa(in.ServerPort)))
	if in.Keepalive > 0 {
		fmt.Fprintf(&sb, "PersistentKeepalive = %d\n", in.Keepalive)
	}
	return sb.String(), nil
}

// QRCodePNG encodes text as a square QR code image.
func QRCodePNG(text string, size int) ([]byte, error) {
	return qrcode.Encode(text, qrcode.Medium, size)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// ConfigFileName makes a safe file name such as "sams-iphone.conf".
func ConfigFileName(deviceName string) string {
	s := strings.ToLower(strings.ReplaceAll(deviceName, "'", ""))
	s = strings.Trim(nonAlnum.ReplaceAllString(s, "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "device"
	}
	return s + ".conf"
}
