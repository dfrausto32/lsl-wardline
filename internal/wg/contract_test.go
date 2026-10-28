package wg

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
)

// The contract suite runs against every Backend implementation. A backend that
// cannot run in the current environment skips with a recorded reason.

var ifaceCounter atomic.Int32

func uniqueName() string {
	return fmt.Sprintf("wgt%d%d", os.Getpid()%1000, ifaceCounter.Add(1))
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

type factory struct {
	name string
	new  func(t *testing.T) Backend // calls t.Skip when unavailable
}

func factories() []factory {
	return []factory{
		{"fake", func(t *testing.T) Backend { return NewFake() }},
		{"userspace", func(t *testing.T) Backend {
			if runtime.GOOS != "linux" {
				t.Skip("userspace contract tests need Linux (tun + netlink)")
			}
			if _, err := os.Stat("/dev/net/tun"); err != nil {
				t.Skip("no /dev/net/tun in this environment")
			}
			return NewUserspace()
		}},
		{"kernel", func(t *testing.T) Backend {
			b := kernelOrSkip(t)
			return b
		}},
	}
}

func TestBackendContract(t *testing.T) {
	for _, f := range factories() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			tests := map[string]func(*testing.T, Backend){
				"ConfigureIsIdempotent":         contractConfigureTwice,
				"AddPeerAndReadBack":            contractAddPeer,
				"ReconcileTwiceIsNoop":          contractReconcileNoop,
				"UpdatePeer":                    contractUpdatePeer,
				"RemovePeer":                    contractRemovePeer,
				"RemoveInterface":               contractRemoveInterface,
				"PeersOfUnknownInterfaceErrors": contractUnknownInterface,
				"PresharedKeyAppliedAndCleared": contractPSK,
			}
			for name, fn := range tests {
				fn := fn
				t.Run(name, func(t *testing.T) {
					b := f.new(t)
					fn(t, b)
				})
			}
		})
	}
}

func newIface(t *testing.T, b Backend) string {
	t.Helper()
	name := uniqueName()
	cfg := InterfaceConfig{PrivateKey: mustKey(t), ListenPort: freeUDPPort(t), Address: netip.MustParsePrefix("10.211.0.1/24")}
	if err := b.Configure(context.Background(), name, cfg); err != nil {
		t.Fatalf("configure: %v", err)
	}
	t.Cleanup(func() { b.Remove(context.Background(), name) })
	return name
}

func ips(s ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, x := range s {
		out = append(out, netip.MustParsePrefix(x))
	}
	return out
}

func contractConfigureTwice(t *testing.T, b Backend) {
	name := newIface(t, b)
	cfg := InterfaceConfig{PrivateKey: mustKey(t), ListenPort: freeUDPPort(t), Address: netip.MustParsePrefix("10.211.0.1/24")}
	if err := b.Configure(context.Background(), name, cfg); err != nil {
		t.Fatalf("second configure: %v", err)
	}
	peers, err := b.Peers(context.Background(), name)
	if err != nil || len(peers) != 0 {
		t.Fatalf("peers=%v err=%v", peers, err)
	}
}

func contractAddPeer(t *testing.T, b Backend) {
	ctx := context.Background()
	name := newIface(t, b)
	pub := mustPub(t)
	want := PeerConfig{PublicKey: pub, AllowedIPs: ips("10.211.0.2/32"), Keepalive: 25}
	if err := b.ApplyDiff(ctx, name, Diff{Add: []PeerConfig{want}}); err != nil {
		t.Fatal(err)
	}
	got, err := b.Peers(ctx, name)
	if err != nil || len(got) != 1 {
		t.Fatalf("peers=%v err=%v", got, err)
	}
	p := got[0]
	if p.PublicKey != pub || len(p.AllowedIPs) != 1 || p.AllowedIPs[0] != want.AllowedIPs[0] || p.Keepalive != 25 {
		t.Fatalf("read back %+v", p)
	}
	if !p.LastHandshake.IsZero() {
		t.Fatalf("a new peer has never connected: %v", p.LastHandshake)
	}
}

func contractReconcileNoop(t *testing.T, b Backend) {
	ctx := context.Background()
	name := newIface(t, b)
	desired := []PeerConfig{
		{PublicKey: mustPub(t), AllowedIPs: ips("10.211.0.2/32"), Keepalive: 25},
		{PublicKey: mustPub(t), AllowedIPs: ips("10.211.0.3/32")},
	}
	d1, err := Reconcile(ctx, b, name, desired)
	if err != nil || len(d1.Add) != 2 {
		t.Fatalf("first: %+v %v", d1, err)
	}
	d2, err := Reconcile(ctx, b, name, desired)
	if err != nil || !d2.IsEmpty() {
		t.Fatalf("second apply must change nothing: %+v %v", d2, err)
	}
}

func contractUpdatePeer(t *testing.T, b Backend) {
	ctx := context.Background()
	name := newIface(t, b)
	pub := mustPub(t)
	Reconcile(ctx, b, name, []PeerConfig{{PublicKey: pub, AllowedIPs: ips("10.211.0.2/32")}})
	d, err := Reconcile(ctx, b, name, []PeerConfig{{PublicKey: pub, AllowedIPs: ips("10.211.0.9/32"), Keepalive: 10}})
	if err != nil || len(d.Update) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	got, _ := b.Peers(ctx, name)
	if len(got) != 1 || got[0].AllowedIPs[0] != netip.MustParsePrefix("10.211.0.9/32") || got[0].Keepalive != 10 {
		t.Fatalf("%+v", got)
	}
}

func contractRemovePeer(t *testing.T, b Backend) {
	ctx := context.Background()
	name := newIface(t, b)
	a, c := mustPub(t), mustPub(t)
	Reconcile(ctx, b, name, []PeerConfig{{PublicKey: a, AllowedIPs: ips("10.211.0.2/32")}, {PublicKey: c, AllowedIPs: ips("10.211.0.3/32")}})
	d, err := Reconcile(ctx, b, name, []PeerConfig{{PublicKey: c, AllowedIPs: ips("10.211.0.3/32")}})
	if err != nil || len(d.Remove) != 1 || d.Remove[0] != a {
		t.Fatalf("%+v %v", d, err)
	}
	got, _ := b.Peers(ctx, name)
	if len(got) != 1 || got[0].PublicKey != c {
		t.Fatalf("%+v", got)
	}
}

func contractRemoveInterface(t *testing.T, b Backend) {
	ctx := context.Background()
	name := newIface(t, b)
	if err := b.Remove(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Peers(ctx, name); err == nil {
		t.Fatal("interface should be gone")
	}
	if err := b.Remove(ctx, name); err != nil {
		t.Fatalf("removing twice must not fail: %v", err)
	}
}

func contractUnknownInterface(t *testing.T, b Backend) {
	if _, err := b.Peers(context.Background(), "doesnotexist0"); err == nil {
		t.Fatal("expected error")
	}
}

func contractPSK(t *testing.T, b Backend) {
	ctx := context.Background()
	name := newIface(t, b)
	pub := mustPub(t)
	psk, _ := GeneratePresharedKey()
	Reconcile(ctx, b, name, []PeerConfig{{PublicKey: pub, AllowedIPs: ips("10.211.0.2/32"), PresharedKey: psk}})
	got, _ := b.Peers(ctx, name)
	if len(got) != 1 || got[0].PresharedKey != psk {
		t.Fatalf("psk not applied: %+v", got)
	}
	d, err := Reconcile(ctx, b, name, []PeerConfig{{PublicKey: pub, AllowedIPs: ips("10.211.0.2/32")}})
	if err != nil || len(d.Update) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	got, _ = b.Peers(ctx, name)
	if got[0].PresharedKey != "" {
		t.Fatalf("psk not cleared: %+v", got)
	}
}

func TestFakeSimulatesHandshakes(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	f.Configure(ctx, "wg0", InterfaceConfig{PrivateKey: mustKey(t), Address: netip.MustParsePrefix("10.0.0.1/24")})
	pub := mustPub(t)
	f.ApplyDiff(ctx, "wg0", Diff{Add: []PeerConfig{peer(pub, "10.0.0.2/32")}})
	f.SimulateHandshake("wg0", pub, 100, 200, "203.0.113.5:4444")
	got, _ := f.Peers(ctx, "wg0")
	if got[0].LastHandshake.IsZero() || got[0].RxBytes != 100 || got[0].TxBytes != 200 || got[0].LastEndpoint != "203.0.113.5:4444" {
		t.Fatalf("%+v", got[0])
	}
}

func TestKernelAvailabilityIsRecorded(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip()
	}
	if out, err := exec.Command("ip", "link", "add", "wgenvtest2", "type", "wireguard").CombinedOutput(); err != nil {
		t.Logf("KERNEL-WIREGUARD: unavailable: %s", out)
		return
	}
	exec.Command("ip", "link", "del", "wgenvtest2").Run()
	t.Log("KERNEL-WIREGUARD: available")
}
