package wg

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder stands in for wireguard.exe and wg.exe. It never runs anything.
type recorder struct {
	mu      sync.Mutex
	calls   []string // "exe arg arg"
	files   map[string]string
	removed []string
	dump    string // what "wg show <name> dump" prints; "" means the tunnel does not exist
}

func newRecorder() *recorder { return &recorder{files: map[string]string{}} }

func (r *recorder) run(_ context.Context, exe string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, strings.TrimSuffix(exe, ".exe")+" "+strings.Join(args, " "))
	if strings.HasSuffix(exe, "wg.exe") && len(args) >= 3 && args[0] == "show" && args[2] == "dump" {
		if r.dump == "" {
			return nil, errors.New("Unable to access interface: no such device")
		}
		return []byte(r.dump), nil
	}
	return nil, nil
}

func (r *recorder) app() *WireGuardApp {
	return &WireGuardApp{
		WireGuardExe: `C:\Program Files\WireGuard\wireguard.exe`, WgExe: `C:\Program Files\WireGuard\wg.exe`, Dir: "tunnels",
		Run:       r.run,
		WriteFile: func(path string, data []byte) error { r.files[path] = string(data); return nil },
		RemoveFile: func(path string) error {
			r.removed = append(r.removed, path)
			delete(r.files, path)
			return nil
		},
	}
}

func (r *recorder) called(prefix string) []string {
	var out []string
	for _, c := range r.calls {
		if strings.Contains(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

const (
	privA = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	pubS  = "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg="
	pskP  = "FpCyhws9cxwWoV4xELtfJvjJN+zQVRPISllRWgeopVE="
)

func iface() InterfaceConfig {
	return InterfaceConfig{PrivateKey: privA, Address: netip.MustParsePrefix("10.77.5.2/24")}
}

func serverPeer() PeerConfig {
	return PeerConfig{PublicKey: pubS, PresharedKey: pskP, Keepalive: 25, Endpoint: "203.0.113.10:51820",
		AllowedIPs: []netip.Prefix{netip.MustParsePrefix("10.77.5.0/24")}}
}

func TestParseTheOutputOfWgShowDump(t *testing.T) {
	dump := privA + "\t" + "someone" + "\t0\toff\n" +
		pubS + "\t" + pskP + "\t203.0.113.10:51820\t10.77.5.0/24,10.9.0.0/16\t1700000000\t1234\t5678\t25\n" +
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\t(none)\t(none)\t(none)\t0\t0\t0\toff\n"
	peers, err := parseDump(dump)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("%+v", peers)
	}
	p := peers[1] // sorted by key: the all-A key sorts before pubS
	if p.PublicKey != pubS || p.PresharedKey != pskP || p.Endpoint != "203.0.113.10:51820" || p.Keepalive != 25 ||
		p.RxBytes != 1234 || p.TxBytes != 5678 || p.LastHandshake.Unix() != 1700000000 || len(p.AllowedIPs) != 2 {
		t.Fatalf("%+v", p)
	}
	q := peers[0]
	if q.PresharedKey != "" || q.Endpoint != "" || len(q.AllowedIPs) != 0 || q.Keepalive != 0 || !q.LastHandshake.IsZero() {
		t.Fatalf("the empty values must read as empty: %+v", q)
	}
	if _, err := parseDump("garbage"); err == nil {
		t.Fatal("expected an error for output that is not a dump")
	}
}

func TestFirstConfigureWritesAProtectedConfigAndInstallsTheTunnelService(t *testing.T) {
	r := newRecorder()
	a := r.app()
	if err := a.Configure(context.Background(), "wardline-agent", iface()); err != nil {
		t.Fatal(err)
	}
	conf := r.files[`tunnels\wardline-agent.conf`]
	if conf == "" {
		conf = r.files["tunnels/wardline-agent.conf"]
	}
	if !strings.Contains(conf, "PrivateKey = "+privA) || !strings.Contains(conf, "Address = 10.77.5.2/24") || strings.Contains(conf, "[Peer]") {
		t.Fatalf("config:\n%s", conf)
	}
	inst := r.called("/installtunnelservice")
	if len(inst) != 1 {
		t.Fatalf("calls: %v", r.calls)
	}
	// A secret on a command line is visible to every program on the computer.
	for _, c := range r.calls {
		if strings.Contains(c, privA) || strings.Contains(c, pskP) {
			t.Fatalf("a key was passed on a command line: %s", c)
		}
	}
}

func TestPeersAreChangedWithSyncconfAndNotByReinstalling(t *testing.T) {
	r := newRecorder()
	a := r.app()
	ctx := context.Background()
	a.Configure(ctx, "wardline-agent", iface())
	r.dump = privA + "\tx\t0\toff\n" // the tunnel is up and has no peers yet
	r.calls = nil

	if err := a.ApplyDiff(ctx, "wardline-agent", Diff{Add: []PeerConfig{serverPeer()}}); err != nil {
		t.Fatal(err)
	}
	if len(r.called("/installtunnelservice")) != 0 || len(r.called("/uninstalltunnelservice")) != 0 {
		t.Fatalf("a peer change must not restart the tunnel: %v", r.calls)
	}
	sync := r.called("syncconf wardline-agent")
	if len(sync) != 1 {
		t.Fatalf("calls: %v", r.calls)
	}
	var stripped string
	for p, c := range r.files {
		if strings.Contains(p, "sync") {
			stripped = c
		}
	}
	// syncconf takes the plain format: no Address line, and the peer is in it.
	// The temporary file is removed again afterwards, so it is found in what was removed.
	if stripped != "" && strings.Contains(stripped, "Address") {
		t.Fatalf("syncconf config must not contain Address:\n%s", stripped)
	}
	if len(r.removed) == 0 {
		t.Fatal("the temporary config with the keys in it was left behind")
	}
}

func TestSyncconfConfigHasTheWholePeerSetNotJustTheDifference(t *testing.T) {
	r := newRecorder()
	a := r.app()
	var seen string
	a.WriteFile = func(path string, data []byte) error {
		if strings.Contains(path, "sync") {
			seen = string(data)
		}
		r.files[path] = string(data)
		return nil
	}
	ctx := context.Background()
	a.Configure(ctx, "wardline-agent", iface())
	other := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	r.dump = privA + "\tx\t0\toff\n" +
		other + "\t(none)\t198.51.100.1:51820\t10.77.5.9/32\t0\t0\t0\toff\n"
	if err := a.ApplyDiff(ctx, "wardline-agent", Diff{Add: []PeerConfig{serverPeer()}, Remove: []string{other}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "PublicKey = "+pubS) || strings.Contains(seen, other) {
		t.Fatalf("expected the new peer only:\n%s", seen)
	}
	if !strings.Contains(seen, "PresharedKey = "+pskP) || !strings.Contains(seen, "PersistentKeepalive = 25") || !strings.Contains(seen, "Endpoint = 203.0.113.10:51820") {
		t.Fatalf("peer settings missing:\n%s", seen)
	}
}

func TestChangingTheKeyKeepsTheTunnelUpAndAChangedAddressReinstallsIt(t *testing.T) {
	r := newRecorder()
	a := r.app()
	ctx := context.Background()
	a.Configure(ctx, "wardline-agent", iface())
	r.dump = privA + "\tx\t0\toff\n" + pubS + "\t(none)\t203.0.113.10:51820\t10.77.5.0/24\t0\t0\t0\t25\n"
	r.calls = nil

	newer := iface()
	newer.PrivateKey = "6M4lsGz6oAaVhKAL2tw5GmO2Q2x2cQxZb+GSsc5lXXI="
	if err := a.Configure(ctx, "wardline-agent", newer); err != nil {
		t.Fatal(err)
	}
	if len(r.called("/installtunnelservice")) != 0 || len(r.called("syncconf")) != 1 {
		t.Fatalf("a new key should be set without restarting: %v", r.calls)
	}

	r.calls = nil
	moved := newer
	moved.Address = netip.MustParsePrefix("10.88.0.2/24")
	if err := a.Configure(ctx, "wardline-agent", moved); err != nil {
		t.Fatal(err)
	}
	if len(r.called("/uninstalltunnelservice")) != 1 || len(r.called("/installtunnelservice")) != 1 {
		t.Fatalf("a new address needs the tunnel rebuilt: %v", r.calls)
	}
}

func TestAProgramThatRestartedRebuildsTheTunnelSoNothingStaleIsLeft(t *testing.T) {
	r := newRecorder()
	r.dump = privA + "\tx\t0\toff\n" // a tunnel from before the restart is still running
	a := r.app()
	if err := a.Configure(context.Background(), "wardline-agent", iface()); err != nil {
		t.Fatal(err)
	}
	if len(r.called("/uninstalltunnelservice")) != 1 || len(r.called("/installtunnelservice")) != 1 {
		t.Fatalf("calls: %v", r.calls)
	}
}

func TestRemoveUninstallsTheServiceAndDeletesTheConfig(t *testing.T) {
	r := newRecorder()
	a := r.app()
	ctx := context.Background()
	a.Configure(ctx, "wardline-agent", iface())
	if err := a.Remove(ctx, "wardline-agent"); err != nil {
		t.Fatal(err)
	}
	if len(r.called("/uninstalltunnelservice wardline-agent")) != 1 {
		t.Fatalf("calls: %v", r.calls)
	}
	for p := range r.files {
		if strings.HasSuffix(p, "wardline-agent.conf") {
			t.Fatal("the config with the private key was left on disk")
		}
	}
	// Removing a tunnel that is already gone is not an error.
	if err := a.Remove(ctx, "wardline-agent"); err != nil {
		t.Fatal(err)
	}
}

func TestPeersReadsTheLiveTunnel(t *testing.T) {
	r := newRecorder()
	r.dump = privA + "\tx\t0\toff\n" + pubS + "\t" + pskP + "\t203.0.113.10:51820\t10.77.5.0/24\t" +
		itoa(time.Now().Add(-30*time.Second).Unix()) + "\t10\t20\t25\n"
	a := r.app()
	peers, err := a.Peers(context.Background(), "wardline-agent")
	if err != nil || len(peers) != 1 || peers[0].RxBytes != 10 || time.Since(peers[0].LastHandshake) > time.Minute {
		t.Fatalf("%+v %v", peers, err)
	}
	r.dump = ""
	if _, err := a.Peers(context.Background(), "wardline-agent"); err == nil {
		t.Fatal("a tunnel that does not exist should be an error, as for the other backends")
	}
}

func itoa(n int64) string {
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 || i == len(b) {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
