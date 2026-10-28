package render

import (
	"bytes"
	"encoding/base64"
	"flag"
	"image/png"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func keyOf(n byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{n}, 32))
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "render", name)
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file %s (run with -update once and review it): %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("%s differs from golden file.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func serverInput() ServerInput {
	return ServerInput{
		PrivateKey: keyOf(9),
		ListenPort: 51820,
		Address:    netip.MustParsePrefix("10.77.5.1/24"),
		Peers: []ServerPeer{
			{Label: "Sam's laptop", PublicKey: keyOf(3), PresharedKey: keyOf(13), TunnelIP: netip.MustParseAddr("10.77.5.10"), Keepalive: 25},
			{Label: "Sam's iPhone", PublicKey: keyOf(2), PresharedKey: keyOf(12), TunnelIP: netip.MustParseAddr("10.77.5.2")},
		},
	}
}

func TestServerConfigGolden(t *testing.T) {
	got, err := ServerConfig(serverInput())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "server.conf", got)
}

func TestServerPeersAreIsolatedToTheirOwnAddress(t *testing.T) {
	got, _ := ServerConfig(serverInput())
	if strings.Count(got, "AllowedIPs = 10.77.5.") != 2 || strings.Count(got, "/32\n") != 2 {
		t.Fatalf("each peer must only be allowed its own /32:\n%s", got)
	}
	if strings.Contains(got, "AllowedIPs = 10.77.5.0/24") || strings.Contains(got, "0.0.0.0/0") {
		t.Fatalf("a device must not be allowed the whole network:\n%s", got)
	}
}

func phone() DeviceInput {
	return DeviceInput{
		PrivateKey:      keyOf(5),
		PresharedKey:    keyOf(12),
		TunnelIP:        netip.MustParseAddr("10.77.5.2"),
		NetworkRange:    netip.MustParsePrefix("10.77.5.0/24"),
		ServerPublicKey: keyOf(7),
		ServerAddress:   "vpn.example.com",
		ServerPort:      51820,
	}
}

func TestPhoneConfigGolden(t *testing.T) {
	got, err := DeviceConfig(phone())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "PersistentKeepalive") {
		t.Fatal("phones must not use keepalive (battery)")
	}
	golden(t, "device-phone.conf", got)
}

func TestLaptopConfigGolden(t *testing.T) {
	in := phone()
	in.Keepalive = 25
	in.TunnelIP = netip.MustParseAddr("10.77.5.10")
	in.DNS = []netip.Addr{netip.MustParseAddr("10.77.5.1")}
	got, err := DeviceConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "device-laptop.conf", got)
}

func TestIPv6AndIPServerAddressesAreFormattedAsEndpoints(t *testing.T) {
	in := phone()
	in.ServerAddress = "2001:db8::1"
	got, _ := DeviceConfig(in)
	if !strings.Contains(got, "Endpoint = [2001:db8::1]:51820") {
		t.Fatalf("ipv6 endpoint:\n%s", got)
	}
	in.ServerAddress = "203.0.113.9"
	got, _ = DeviceConfig(in)
	if !strings.Contains(got, "Endpoint = 203.0.113.9:51820") {
		t.Fatalf("ipv4 endpoint:\n%s", got)
	}
}

func TestInvalidInputIsRejected(t *testing.T) {
	bad := phone()
	bad.PrivateKey = "nope"
	if _, err := DeviceConfig(bad); err == nil {
		t.Fatal("bad private key")
	}
	bad = phone()
	bad.ServerAddress = "vpn.example.com\nPostUp = rm -rf /"
	if _, err := DeviceConfig(bad); err == nil {
		t.Fatal("server address with a newline must be rejected")
	}
	bad = phone()
	bad.ServerAddress = ""
	if _, err := DeviceConfig(bad); err == nil {
		t.Fatal("missing server address")
	}
	s := serverInput()
	s.Peers[0].PublicKey = "x"
	if _, err := ServerConfig(s); err == nil {
		t.Fatal("bad peer key")
	}
}

func TestLabelsCannotInjectConfigLines(t *testing.T) {
	s := serverInput()
	s.Peers[0].Label = "Evil\n[Peer]\nPublicKey = " + keyOf(66) + "\nAllowedIPs = 0.0.0.0/0"
	got, err := ServerConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	sections := 0
	for _, line := range strings.Split(got, "\n") {
		if line == "[Peer]" {
			sections++
		}
	}
	if sections != 2 {
		t.Fatalf("label injected a peer section:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "PublicKey = "+keyOf(66)) || strings.HasPrefix(line, "AllowedIPs = 0.0.0.0") {
			t.Fatalf("label injected a config line: %q", line)
		}
	}
}

func TestQRCodeIsASquarePNG(t *testing.T) {
	cfg, _ := DeviceConfig(phone())
	b, err := QRCodePNG(cfg, 320)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != img.Bounds().Dy() || img.Bounds().Dx() < 200 {
		t.Fatalf("bounds %v", img.Bounds())
	}
}

func TestConfigFileName(t *testing.T) {
	cases := map[string]string{
		"Sam's iPhone":     "sams-iphone.conf",
		"  Work / Laptop ": "work-laptop.conf",
		"../../etc/passwd": "etc-passwd.conf",
		"!!!":              "device.conf",
		"A very long name that goes on and on and on and on and on and on": "a-very-long-name-that-goes-on-and-on-and.conf",
	}
	for in, want := range cases {
		if got := ConfigFileName(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDesiredPeersMatchesServerConfig(t *testing.T) {
	peers := DesiredPeers(serverInput().Peers)
	if len(peers) != 2 {
		t.Fatal(len(peers))
	}
	for _, p := range peers {
		if len(p.AllowedIPs) != 1 || p.AllowedIPs[0].Bits() != 32 {
			t.Fatalf("peer allowed ips: %v", p.AllowedIPs)
		}
	}
}
