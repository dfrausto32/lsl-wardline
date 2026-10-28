package gateway

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	wlog "lsl-wardline/internal/log"
	"lsl-wardline/internal/wg"
)

// ---- test doubles ----

type fakeControl struct {
	mu        sync.Mutex
	pub       ed25519.PublicKey
	priv      ed25519.PrivateKey
	signed    Signed
	version   int64
	enrolls   []EnrollRequest
	reports   []Report
	changed   chan struct{}
	failToken bool
}

func newFakeControl(t *testing.T) *fakeControl {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &fakeControl{pub: pub, priv: priv, changed: make(chan struct{}, 16)}
}

func (c *fakeControl) set(t *testing.T, st DesiredState) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	s, err := Sign(c.priv, st)
	if err != nil {
		t.Fatal(err)
	}
	c.signed, c.version = s, st.Version
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

func (c *fakeControl) Enroll(_ context.Context, r EnrollRequest) (EnrollResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failToken || r.Token != "good-token" {
		return EnrollResponse{}, errors.New("token not accepted")
	}
	c.enrolls = append(c.enrolls, r)
	return EnrollResponse{ServerID: "srv1", AuthToken: "auth-abc", ControlKey: c.pub}, nil
}

func (c *fakeControl) Desired(ctx context.Context, _ string, since int64) (Signed, bool, error) {
	deadline := time.After(150 * time.Millisecond)
	for {
		c.mu.Lock()
		v, s := c.version, c.signed
		c.mu.Unlock()
		if v > since || since < 0 && v > 0 {
			return s, true, nil
		}
		select {
		case <-c.changed:
		case <-deadline:
			return Signed{}, false, nil
		case <-ctx.Done():
			return Signed{}, false, ctx.Err()
		}
	}
}

func (c *fakeControl) Report(_ context.Context, _ string, r Report) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reports = append(c.reports, r)
	return nil
}

// countingBackend counts how often something is actually changed.
type countingBackend struct {
	*wg.Fake
	mu     sync.Mutex
	applys int
}

func (b *countingBackend) ApplyDiff(ctx context.Context, name string, d wg.Diff) error {
	b.mu.Lock()
	b.applys++
	b.mu.Unlock()
	return b.Fake.ApplyDiff(ctx, name, d)
}

func (b *countingBackend) count() int { b.mu.Lock(); defer b.mu.Unlock(); return b.applys }

func pubKey(t *testing.T) string {
	t.Helper()
	_, p, err := wg.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func state(version int64, peers ...DesiredPeer) DesiredState {
	return DesiredState{Version: version, IssuedAt: time.Now().UTC(), ServerID: "srv1", Address: "10.77.5.1/24", ListenPort: 51820, Peers: peers}
}

func newGateway(t *testing.T, c Control, dir string) (*Gateway, *countingBackend, *bytes.Buffer) {
	t.Helper()
	b := &countingBackend{Fake: wg.NewFake()}
	var logs bytes.Buffer
	g := New(Config{
		StateDir: dir, Iface: "wardline0", Name: "Server 1", EnrollToken: "good-token", ControlURL: "https://control.example",
		Control: c, Backend: b, Log: wlog.New(&logs, "debug"), ReportEvery: 20 * time.Millisecond,
	})
	return g, b, &logs
}

// ---- enrolment ----

func TestEnrollSendsOnlyThePublicKeyAndStoresIdentityPrivately(t *testing.T) {
	c := newFakeControl(t)
	dir := t.TempDir()
	g, _, logs := newGateway(t, c, dir)
	if err := g.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.enrolls) != 1 {
		t.Fatalf("enrolls = %d", len(c.enrolls))
	}
	sent := c.enrolls[0]
	if err := wg.ValidateKey(sent.PublicKey); err != nil {
		t.Fatalf("public key: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var id Identity
	json.Unmarshal(raw, &id)
	if id.PrivateKey == "" || id.PrivateKey == sent.PublicKey {
		t.Fatal("private key should be stored locally and differ from the public key")
	}
	payload, _ := json.Marshal(sent)
	if strings.Contains(string(payload), id.PrivateKey) {
		t.Fatal("private key was sent to the control plane")
	}
	if strings.Contains(logs.String(), id.PrivateKey) {
		t.Fatal("private key reached the logs")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "identity.json")); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("identity file is readable by others: %v", fi.Mode())
	}
}

func TestEnrollWithBadTokenFailsClearly(t *testing.T) {
	c := newFakeControl(t)
	g, _, _ := newGateway(t, c, t.TempDir())
	g.cfg.EnrollToken = "wrong"
	err := g.Start(context.Background())
	if err == nil {
		t.Fatal("expected failure")
	}
}

func TestRestartDoesNotEnrollAgain(t *testing.T) {
	c := newFakeControl(t)
	dir := t.TempDir()
	g1, _, _ := newGateway(t, c, dir)
	g1.Start(context.Background())
	g2, _, _ := newGateway(t, c, dir)
	g2.cfg.EnrollToken = "" // already enrolled; no token needed
	if err := g2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.enrolls) != 1 {
		t.Fatalf("enrolled %d times", len(c.enrolls))
	}
	if g1.PublicKey() != g2.PublicKey() {
		t.Fatal("identity changed across restart")
	}
}

// ---- applying state ----

func TestSyncAppliesDesiredStateAndIsIdempotent(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	ctx := context.Background()
	g.Start(ctx)
	k1, k2 := pubKey(t), pubKey(t)
	c.set(t, state(1,
		DesiredPeer{DeviceID: "d1", PublicKey: k1, TunnelIP: "10.77.5.2"},
		DesiredPeer{DeviceID: "d2", PublicKey: k2, PresharedKey: "", TunnelIP: "10.77.5.3", Keepalive: 25}))

	d, err := g.Sync(ctx)
	if err != nil || len(d.Add) != 2 {
		t.Fatalf("first sync: %+v %v", d, err)
	}
	peers, _ := b.Peers(ctx, "wardline0")
	if len(peers) != 2 || peers[0].AllowedIPs[0].String() == "" {
		t.Fatalf("%+v", peers)
	}
	before := b.count()
	d, err = g.Sync(ctx)
	if err != nil || !d.IsEmpty() {
		t.Fatalf("second sync must be empty: %+v %v", d, err)
	}
	if b.count() != before {
		t.Fatal("second sync changed the live interface")
	}
}

func TestSyncRemovesDevicesThatLeftTheDesiredState(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	ctx := context.Background()
	g.Start(ctx)
	k1, k2 := pubKey(t), pubKey(t)
	c.set(t, state(1, DesiredPeer{DeviceID: "d1", PublicKey: k1, TunnelIP: "10.77.5.2"}, DesiredPeer{DeviceID: "d2", PublicKey: k2, TunnelIP: "10.77.5.3"}))
	g.Sync(ctx)
	c.set(t, state(2, DesiredPeer{DeviceID: "d2", PublicKey: k2, TunnelIP: "10.77.5.3"}))
	d, err := g.Sync(ctx)
	if err != nil || len(d.Remove) != 1 || d.Remove[0] != k1 {
		t.Fatalf("%+v %v", d, err)
	}
	peers, _ := b.Peers(ctx, "wardline0")
	if len(peers) != 1 {
		t.Fatalf("%+v", peers)
	}
}

func TestBadSignatureIsRejectedAndNothingChanges(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	ctx := context.Background()
	g.Start(ctx)
	k := pubKey(t)
	c.set(t, state(1, DesiredPeer{DeviceID: "d1", PublicKey: k, TunnelIP: "10.77.5.2"}))
	g.Sync(ctx)

	// An attacker (or a broken proxy) serves a state signed with another key.
	_, evil, _ := ed25519.GenerateKey(rand.Reader)
	forged, _ := Sign(evil, state(2, DesiredPeer{DeviceID: "x", PublicKey: pubKey(t), TunnelIP: "10.77.5.99"}))
	c.mu.Lock()
	c.signed, c.version = forged, 2
	c.mu.Unlock()

	if _, err := g.Sync(ctx); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
	peers, _ := b.Peers(ctx, "wardline0")
	if len(peers) != 1 || peers[0].PublicKey != k {
		t.Fatalf("live state changed: %+v", peers)
	}
}

func TestTamperedPayloadIsRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, _ := Sign(priv, state(1))
	s.Payload = bytes.Replace(s.Payload, []byte(`"version":1`), []byte(`"version":9`), 1)
	if _, err := Verify(pub, s); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v", err)
	}
}

func TestOldVersionIsRejectedEvenAfterRestart(t *testing.T) {
	c := newFakeControl(t)
	dir := t.TempDir()
	g, _, _ := newGateway(t, c, dir)
	ctx := context.Background()
	g.Start(ctx)
	c.set(t, state(5))
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	// Replay an older, validly signed state.
	old, _ := Sign(c.priv, state(3, DesiredPeer{DeviceID: "d1", PublicKey: pubKey(t), TunnelIP: "10.77.5.2"}))
	c.mu.Lock()
	c.signed, c.version = old, 6 // pretend the version counter moved so the client asks for it
	c.mu.Unlock()
	if _, err := g.Sync(ctx); !errors.Is(err, ErrStaleState) {
		t.Fatalf("want ErrStaleState, got %v", err)
	}

	g2, _, _ := newGateway(t, c, dir)
	g2.Start(ctx)
	if _, err := g2.Sync(ctx); !errors.Is(err, ErrStaleState) {
		t.Fatalf("restarted gateway forgot the last version: %v", err)
	}
}

func TestRestartReappliesSameVersion(t *testing.T) {
	c := newFakeControl(t)
	dir := t.TempDir()
	g, _, _ := newGateway(t, c, dir)
	ctx := context.Background()
	g.Start(ctx)
	k := pubKey(t)
	c.set(t, state(1, DesiredPeer{DeviceID: "d1", PublicKey: k, TunnelIP: "10.77.5.2"}))
	g.Sync(ctx)

	g2, b2, _ := newGateway(t, c, dir) // fresh backend: the interface is gone after a reboot
	g2.Start(ctx)
	d, err := g2.Sync(ctx)
	if err != nil || len(d.Add) != 1 {
		t.Fatalf("a restarted server must rebuild its interface: %+v %v", d, err)
	}
	if peers, _ := b2.Peers(ctx, "wardline0"); len(peers) != 1 {
		t.Fatal("peer missing after restart")
	}
}

// ---- reporting ----

func TestReportCarriesHandshakeAndTraffic(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	ctx := context.Background()
	g.Start(ctx)
	k := pubKey(t)
	c.set(t, state(1, DesiredPeer{DeviceID: "d1", PublicKey: k, TunnelIP: "10.77.5.2"}))
	g.Sync(ctx)
	b.SimulateHandshake("wardline0", k, 1234, 5678, "198.51.100.7:40000")

	if err := g.ReportNow(ctx); err != nil {
		t.Fatal(err)
	}
	if len(c.reports) != 1 {
		t.Fatalf("reports: %d", len(c.reports))
	}
	p := c.reports[0].Peers
	if len(p) != 1 || p[0].DeviceID != "d1" || p[0].RxBytes != 1234 || p[0].TxBytes != 5678 || p[0].Endpoint != "198.51.100.7:40000" || p[0].LastHandshake.IsZero() {
		t.Fatalf("%+v", p)
	}
	if c.reports[0].ServerID != "srv1" {
		t.Fatalf("server id %q", c.reports[0].ServerID)
	}
}

func TestReportDoesNotIncludeKeysOtherThanPublic(t *testing.T) {
	c := newFakeControl(t)
	g, _, _ := newGateway(t, c, t.TempDir())
	ctx := context.Background()
	g.Start(ctx)
	c.set(t, state(1, DesiredPeer{DeviceID: "d1", PublicKey: pubKey(t), PresharedKey: "", TunnelIP: "10.77.5.2"}))
	g.Sync(ctx)
	g.ReportNow(ctx)
	raw, _ := json.Marshal(c.reports)
	if strings.Contains(string(raw), g.id.PrivateKey) {
		t.Fatal("private key in report")
	}
}

// ---- the running loop ----

func TestRunAppliesChangesQuicklyAndKeepsReporting(t *testing.T) {
	c := newFakeControl(t)
	g, b, _ := newGateway(t, c, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()

	k := pubKey(t)
	c.set(t, state(1, DesiredPeer{DeviceID: "d1", PublicKey: k, TunnelIP: "10.77.5.2"}))
	waitFor(t, func() bool { p, _ := b.Peers(ctx, "wardline0"); return len(p) == 1 })

	// Removal must not wait for a schedule.
	start := time.Now()
	c.set(t, state(2))
	waitFor(t, func() bool { p, _ := b.Peers(ctx, "wardline0"); return len(p) == 0 })
	if time.Since(start) > time.Second {
		t.Fatalf("removal took %v", time.Since(start))
	}
	waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.reports) >= 2 })
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// ---- HTTP client ----

func TestHTTPClientSpeaksTheGatewayAPI(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /gateway/v1/enroll", func(w http.ResponseWriter, r *http.Request) {
		var req EnrollRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Token != "tok" {
			http.Error(w, `{"error":"nope"}`, 401)
			return
		}
		json.NewEncoder(w).Encode(EnrollResponse{ServerID: "s9", AuthToken: "bearer-1", ControlKey: pub})
	})
	mux.HandleFunc("GET /gateway/v1/desired", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bearer-1" {
			http.Error(w, "no", 401)
			return
		}
		if r.URL.Query().Get("since") == "7" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s, _ := Sign(priv, state(7))
		json.NewEncoder(w).Encode(s)
	})
	mux.HandleFunc("POST /gateway/v1/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bearer-1" {
			http.Error(w, "no", 401)
			return
		}
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cl := NewHTTPControl(srv.URL, srv.Client())
	ctx := context.Background()
	if _, err := cl.Enroll(ctx, EnrollRequest{Token: "bad"}); err == nil {
		t.Fatal("bad token should fail")
	}
	res, err := cl.Enroll(ctx, EnrollRequest{Token: "tok", PublicKey: "k", Name: "n"})
	if err != nil || res.ServerID != "s9" || !bytes.Equal(res.ControlKey, pub) {
		t.Fatalf("%+v %v", res, err)
	}
	s, changed, err := cl.Desired(ctx, res.AuthToken, -1)
	if err != nil || !changed {
		t.Fatalf("desired: %v %v", changed, err)
	}
	if st, err := Verify(pub, s); err != nil || st.Version != 7 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, changed, err := cl.Desired(ctx, res.AuthToken, 7); err != nil || changed {
		t.Fatalf("no change expected: %v %v", changed, err)
	}
	if err := cl.Report(ctx, res.AuthToken, Report{ServerID: "s9"}); err != nil {
		t.Fatal(err)
	}
	if err := cl.Report(ctx, "wrong", Report{}); err == nil {
		t.Fatal("wrong auth should fail")
	}
}
