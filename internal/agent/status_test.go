package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheAgentWritesAStatusFileForTheTrayAndTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status", "status.json")
	r := newRig(t, func(c *Config) { c.StatusPath = path })
	ctx := context.Background()
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	r.fake.SimulateHandshake("wardline-agent", r.ctl.serverPub, 100, 200, "203.0.113.10:51820")
	if err := r.a.WriteStatus(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := ReadStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Connected || s.Address != "10.77.5.2/24" || s.RxBytes != 100 || s.TxBytes != 200 || s.Version != "test" || s.Interface != "wardline-agent" {
		t.Fatalf("%+v", s)
	}
	if s.Server == "" || s.ControlURL != "https://control.example" {
		t.Fatalf("the window needs to say where it is connected: %+v", s)
	}
}

// Every user of the computer can read this file, so it must hold nothing secret.
func TestTheStatusFileHoldsNoSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	r := newRig(t, func(c *Config) { c.StatusPath = path })
	ctx := context.Background()
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.a.WriteStatus(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	r.a.mu.Lock()
	secrets := []string{r.a.id.AuthToken, r.a.id.PrivateKey, string(r.a.id.ControlKey)}
	r.a.mu.Unlock()
	for _, s := range secrets {
		if s != "" && strings.Contains(string(b), s) {
			t.Fatalf("the status file contains a secret: %q", s)
		}
	}
	for _, word := range []string{"auth_token", "private_key", "PrivateKey", "AuthToken"} {
		if strings.Contains(string(b), word) {
			t.Fatalf("the status file mentions %q", word)
		}
	}
}

func TestNoStatusFileIsWrittenUnlessAskedFor(t *testing.T) {
	r := newRig(t)
	if err := r.a.WriteStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDescribingTheStatusInPlainWords(t *testing.T) {
	now := time.Now()
	fresh := func(mod func(*Status)) Status {
		s := Status{Time: now.Add(-2 * time.Second), Connected: true, Address: "10.1.2.3/24"}
		mod(&s)
		return s
	}
	cases := []struct {
		name     string
		s        Status
		state    State
		contains string
	}{
		{"connected", fresh(func(*Status) {}), StateConnected, "Connected"},
		{"still connecting", fresh(func(s *Status) { s.Connected = false }), StateConnecting, "Connecting"},
		{"server unreachable", fresh(func(s *Status) { s.Connected = false; s.Problem = ProblemServerUnreachable }), StateProblem, "reach the server"},
		{"name not found", fresh(func(s *Status) { s.Connected = false; s.Problem = ProblemNameNotFound }), StateProblem, "find the server"},
		{"refresh failed but still connected", fresh(func(s *Status) { s.Problem = ProblemRefreshFailed }), StateProblem, "security"},
		{"the service stopped writing", fresh(func(s *Status) { s.Time = now.Add(-time.Minute) }), StateOff, "not running"},
		{"never written", Status{}, StateOff, "not running"},
	}
	for _, c := range cases {
		state, words := c.s.Describe(now)
		if state != c.state || !strings.Contains(words, c.contains) {
			t.Errorf("%s: got %q %q, want %q containing %q", c.name, state, words, c.state, c.contains)
		}
	}
}

func TestReadingAMissingOrDamagedFileSaysNotRunning(t *testing.T) {
	dir := t.TempDir()
	if s, _ := LoadStatus(filepath.Join(dir, "none.json")); s.Time != (time.Time{}) {
		t.Fatal("a missing file is an empty status")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o644)
	s, _ := LoadStatus(bad)
	if state, _ := s.Describe(time.Now()); state != StateOff {
		t.Fatalf("got %q", state)
	}
}
