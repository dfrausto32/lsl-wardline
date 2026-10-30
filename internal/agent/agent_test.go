package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"lsl-wardline/internal/gateway"
	wlog "lsl-wardline/internal/log"
	"lsl-wardline/internal/wg"
)

// fakeControl stands in for the control plane. It remembers everything the agent sent it.
type fakeControl struct {
	mu         sync.Mutex
	priv       ed25519.PrivateKey
	pub        ed25519.PublicKey
	serverPub  string
	version    int64
	current    string // the key the "control plane" accepts
	previous   string
	revoked    bool
	refreshErr error
	refreshDue *time.Time
	endpoint   string

	namesDomain, namesServer string // when set, the state tells the computer to use the network's names

	enrolls   []EnrollRequest
	refreshed []string
	rollbacks int
	reports   []Report
	sent      [][]byte // every request body, to prove no private key was sent
	onRefresh func(newPub string)
}

func newFakeControl(t *testing.T) *fakeControl {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, spub, _ := wg.GenerateKeypair()
	return &fakeControl{priv: priv, pub: pub, serverPub: spub, version: 5, endpoint: "vpn.example.com:51820"}
}

func (c *fakeControl) record(v any) {
	b, _ := json.Marshal(v)
	c.sent = append(c.sent, b)
}

func (c *fakeControl) Enroll(_ context.Context, r EnrollRequest) (EnrollResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record(r)
	if r.Token != "good-code" {
		return EnrollResponse{}, errors.New("That setup code is not valid or has expired.")
	}
	c.enrolls = append(c.enrolls, r)
	c.current = r.PublicKey
	return EnrollResponse{DeviceID: "dev1", AuthToken: "access-token", ControlKey: c.pub}, nil
}

func (c *fakeControl) state() DesiredState {
	return DesiredState{
		Version: c.version, IssuedAt: time.Now(), DeviceID: "dev1", Address: "10.77.5.2/24",
		ServerPublicKey: c.serverPub, Endpoint: c.endpoint, AllowedIPs: []string{"10.77.5.0/24"},
		PresharedKey: "", Keepalive: 25, PublicKey: c.current, RefreshDue: c.refreshDue,
		NamesDomain: c.namesDomain, NamesServer: c.namesServer,
	}
}

func (c *fakeControl) Desired(ctx context.Context, auth string, since int64) (gateway.Signed, bool, error) {
	c.mu.Lock()
	revoked, changed := c.revoked, since < 0 || c.version > since
	st := c.state()
	priv := c.priv
	c.mu.Unlock()
	if auth != "access-token" || revoked {
		return gateway.Signed{}, false, ErrRevoked
	}
	if !changed {
		select {
		case <-ctx.Done():
		case <-time.After(20 * time.Millisecond):
		}
		return gateway.Signed{}, false, nil
	}
	s, err := Sign(priv, st)
	return s, true, err
}

func (c *fakeControl) Report(_ context.Context, _ string, r Report) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record(r)
	c.reports = append(c.reports, r)
	return nil
}

func (c *fakeControl) Refresh(_ context.Context, _ string, newPub string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record(RefreshRequest{PublicKey: newPub})
	if c.refreshErr != nil {
		return c.refreshErr
	}
	c.previous, c.current = c.current, newPub
	c.refreshed = append(c.refreshed, newPub)
	c.version++
	if c.onRefresh != nil {
		go c.onRefresh(newPub)
	}
	return nil
}

func (c *fakeControl) Rollback(_ context.Context, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.previous
	c.rollbacks++
	c.version++
	return nil
}

func (c *fakeControl) set(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f()
}

type rig struct {
	t     *testing.T
	ctl   *fakeControl
	fake  *wg.Fake
	dir   string
	a     *Agent
	logs  *strings.Builder
	hosts map[string][]netip.Addr
	mu    sync.Mutex
}

func newRig(t *testing.T, mutate ...func(*Config)) *rig {
	t.Helper()
	r := &rig{t: t, ctl: newFakeControl(t), fake: wg.NewFake(), dir: t.TempDir(), logs: &strings.Builder{},
		hosts: map[string][]netip.Addr{"vpn.example.com": {netip.MustParseAddr("203.0.113.10")}}}
	cfg := Config{
		StateDir: r.dir, Iface: "wardline-agent", EnrollToken: "good-code", ControlURL: "https://control.example",
		Control: r.ctl, Backend: r.fake, Log: wlog.New(&lockedWriter{b: r.logs}, "debug"), Version: "test",
		HandshakeWait: 400 * time.Millisecond, ReportEvery: time.Hour,
		Resolver: func(_ context.Context, host string) ([]netip.Addr, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if a, ok := r.hosts[host]; ok && len(a) > 0 {
				return a, nil
			}
			return nil, errors.New("no such host")
		},
	}
	for _, m := range mutate {
		m(&cfg)
	}
	r.a = New(cfg)
	return r
}

type lockedWriter struct {
	mu sync.Mutex
	b  *strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (r *rig) setHost(host string, ips ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var a []netip.Addr
	for _, s := range ips {
		a = append(a, netip.MustParseAddr(s))
	}
	r.hosts[host] = a
}

func (r *rig) peers() []wg.PeerState {
	r.t.Helper()
	p, err := r.fake.Peers(context.Background(), "wardline-agent")
	if err != nil {
		r.t.Fatal(err)
	}
	return p
}

func newKey(t *testing.T) (string, string, error) {
	t.Helper()
	return wg.GenerateKeypair()
}

// ---- enrolling ----

func TestFirstStartEnrollsOnceAndSendsOnlyAPublicKey(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if err := r.a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.ctl.enrolls) != 1 || wg.ValidateKey(r.ctl.enrolls[0].PublicKey) != nil || r.ctl.enrolls[0].AgentVersion != "test" {
		t.Fatalf("%+v", r.ctl.enrolls)
	}
	id := r.a.identity()
	if id.PrivateKey == "" || id.DeviceID != "dev1" || id.AuthToken != "access-token" {
		t.Fatalf("%+v", id)
	}
	for _, b := range r.ctl.sent {
		if strings.Contains(string(b), id.PrivateKey) {
			t.Fatal("the private key was sent to the control plane")
		}
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(r.dir, "agent.json"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("identity file: %v %v", fi, err)
		}
	}
	if strings.Contains(r.logs.String(), id.PrivateKey) || strings.Contains(r.logs.String(), id.AuthToken) {
		t.Fatal("a secret reached the logs")
	}

	// A second agent on the same folder loads the identity and does not enroll again, even with no code.
	cfg := r.a.cfg
	cfg.EnrollToken = ""
	again := New(cfg)
	if err := again.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.ctl.enrolls) != 1 || again.identity().PublicKey != id.PublicKey {
		t.Fatal("it enrolled again or lost its key")
	}
}

func TestEnrollingWithABadCodeSaysSoPlainlyAndSavesNothing(t *testing.T) {
	r := newRig(t, func(c *Config) { c.EnrollToken = "wrong" })
	err := r.a.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "agent.json")); err == nil {
		t.Fatal("an identity was saved even though enrollment failed")
	}
	none := newRig(t, func(c *Config) { c.EnrollToken = "" })
	if err := none.a.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("a never-set-up agent should say how to set it up: %v", err)
	}
}

// ---- applying instructions ----

func TestSyncBuildsTheTunnelAndRunningItAgainChangesNothing(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	d, err := r.a.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Add) != 1 {
		t.Fatalf("first sync: %+v", d)
	}
	cfg, ok := r.fake.Interface("wardline-agent")
	id := r.a.identity()
	if !ok || cfg.PrivateKey != id.PrivateKey || cfg.Address != netip.MustParsePrefix("10.77.5.2/24") || cfg.ListenPort != 0 {
		t.Fatalf("interface: %+v", cfg)
	}
	p := r.peers()
	if len(p) != 1 || p[0].PublicKey != r.ctl.serverPub || p[0].Keepalive != 25 || p[0].Endpoint != "203.0.113.10:51820" ||
		len(p[0].AllowedIPs) != 1 || p[0].AllowedIPs[0] != netip.MustParsePrefix("10.77.5.0/24") {
		t.Fatalf("peer: %+v", p)
	}
	d2, err := r.a.Sync(ctx)
	if err != nil || !d2.IsEmpty() {
		t.Fatalf("second sync should change nothing: %+v %v", d2, err)
	}
}

func TestInstructionsFromSomeoneElseOrOlderOnesAreRefused(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// Stale: the control plane's version went backwards.
	r.ctl.set(func() { r.ctl.version = 2 })
	if _, err := r.a.Sync(ctx); !errors.Is(err, gateway.ErrStaleState) {
		t.Fatalf("stale: %v", err)
	}
	// Signed by a different key.
	r.ctl.set(func() {
		r.ctl.version = 9
		_, other, _ := ed25519.GenerateKey(rand.Reader)
		r.ctl.priv = other
	})
	if _, err := r.a.Sync(ctx); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong signer: %v", err)
	}
	if len(r.peers()) != 1 {
		t.Fatal("a refused instruction changed the tunnel")
	}
}

func TestARemovedDeviceTearsDownItsTunnelAndForgetsItsKey(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.a.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	r.ctl.set(func() { r.ctl.revoked = true; r.ctl.version++ })
	select {
	case err := <-done:
		if !errors.Is(err, ErrRevoked) {
			t.Fatalf("Run returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("the agent did not notice it was removed")
	}
	if _, err := r.fake.Peers(context.Background(), "wardline-agent"); err == nil {
		t.Fatal("the tunnel is still there after removal")
	}
	if _, err := os.Stat(filepath.Join(r.dir, "agent.json")); err == nil {
		t.Fatal("the private key was kept after removal")
	}
}

func TestStoppingTheAgentTakesTheTunnelDownButKeepsTheKey(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.a.Run(ctx) }()
	time.Sleep(100 * time.Millisecond)
	cancel() // what stopping the service does
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not stop")
	}
	if _, err := r.fake.Peers(context.Background(), "wardline-agent"); err == nil {
		t.Fatal("the tunnel is still up after the agent stopped, so the computer is still on the network")
	}
	if _, err := os.Stat(filepath.Join(r.dir, "agent.json")); err != nil {
		t.Fatal("stopping must keep this computer's identity so it can come back on")
	}
	// Turning it back on brings the tunnel back without a new code.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err := r.a.Sync(ctx2); err != nil {
		t.Fatalf("could not come back on: %v", err)
	}
	if len(r.peers()) != 1 {
		t.Fatal("the tunnel did not come back")
	}
}

func TestAgentReportsItsOwnHealthInPlainTerms(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.a.ReportNow(ctx); err != nil {
		t.Fatal(err)
	}
	rep := r.ctl.reports[len(r.ctl.reports)-1]
	if rep.DeviceID != "dev1" || rep.AppliedVersion != 5 || rep.AgentVersion != "test" || !rep.LastHandshake.IsZero() {
		t.Fatalf("%+v", rep)
	}
	// Connected: a recent handshake with the server.
	r.fake.SimulateHandshake("wardline-agent", r.ctl.serverPub, 100, 200, "203.0.113.10:51820")
	r.a.ReportNow(ctx)
	rep = r.ctl.reports[len(r.ctl.reports)-1]
	if !rep.TunnelUp || rep.RxBytes != 100 || rep.TxBytes != 200 || rep.Problem != ProblemNone {
		t.Fatalf("%+v", rep)
	}
	// Not connected for a long while: the agent says the server cannot be reached.
	r.fake.SetHandshake("wardline-agent", r.ctl.serverPub, time.Now().Add(-20*time.Minute))
	r.a.ReportNow(ctx)
	rep = r.ctl.reports[len(r.ctl.reports)-1]
	if rep.TunnelUp || rep.Problem != ProblemServerUnreachable {
		t.Fatalf("%+v", rep)
	}
}
