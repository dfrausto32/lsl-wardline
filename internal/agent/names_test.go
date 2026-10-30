package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeNames stands in for the computer's own name settings.
type fakeNames struct {
	mu      sync.Mutex
	applied []string // "iface domain server"
	cleared int
	fail    error
}

func (f *fakeNames) Apply(_ context.Context, iface, domain, server string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.applied = append(f.applied, iface+" "+domain+" "+server)
	return nil
}

func (f *fakeNames) Clear(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared++
	return nil
}

func (f *fakeNames) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied), f.cleared
}

func lastReport(r *rig) Report {
	r.t.Helper()
	rep, _ := r.a.measure(context.Background())
	return rep
}

func TestTheComputerIsSetToUseNamesWhileTheyAreOnAndNotWhenTheyAreOff(t *testing.T) {
	f := &fakeNames{}
	r := newRig(t, func(c *Config) { c.Names = f })
	ctx := context.Background()

	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if lastReport(r).Names != "" {
		t.Fatal("while names are off the computer reports nothing about them")
	}

	r.ctl.set(func() { r.ctl.namesDomain, r.ctl.namesServer = "wardline.internal", "10.77.5.1"; r.ctl.version++ })
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.counts(); n != 1 || f.applied[0] != "wardline-agent wardline.internal 10.77.5.1" {
		t.Fatalf("applied %v", f.applied)
	}
	if lastReport(r).Names != NamesOK {
		t.Fatalf("report says %q", lastReport(r).Names)
	}

	r.ctl.set(func() { r.ctl.namesDomain, r.ctl.namesServer = "", ""; r.ctl.version++ })
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, cleared := f.counts(); cleared == 0 {
		t.Fatal("turning names off must take the setting off the computer")
	}
	if lastReport(r).Names != "" {
		t.Fatal("report should be empty again")
	}
}

func TestAComputerThatCannotUseNamesSaysSoAndKeepsWorking(t *testing.T) {
	f := &fakeNames{fail: errors.New("this computer has no systemd-resolved")}
	r := newRig(t, func(c *Config) { c.Names = f })
	r.ctl.set(func() { r.ctl.namesDomain, r.ctl.namesServer = "wardline.internal", "10.77.5.1" })
	if _, err := r.a.Sync(context.Background()); err != nil {
		t.Fatalf("names failing must not break the tunnel: %v", err)
	}
	if len(r.peers()) != 1 {
		t.Fatal("the tunnel should still be up")
	}
	if got := lastReport(r).Names; got != NamesUnavailable {
		t.Fatalf("report says %q", got)
	}
	if rep := lastReport(r); rep.Problem != ProblemNone {
		t.Fatalf("not having names is not a connection problem: %q", rep.Problem)
	}
}

func TestAComputerWithNoWayToSetNamesSaysSoToo(t *testing.T) {
	r := newRig(t) // no Names client at all (for example macOS)
	r.ctl.set(func() { r.ctl.namesDomain, r.ctl.namesServer = "wardline.internal", "10.77.5.1" })
	if _, err := r.a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := lastReport(r).Names; got != NamesUnavailable {
		t.Fatalf("report says %q", got)
	}
}

func TestNamesComeOffWhenTheAgentStops(t *testing.T) {
	f := &fakeNames{}
	r := newRig(t, func(c *Config) { c.Names = f })
	r.ctl.set(func() { r.ctl.namesDomain, r.ctl.namesServer = "wardline.internal", "10.77.5.1" })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.a.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if n, _ := f.counts(); n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("names were never applied")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, before := f.counts()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not stop")
	}
	if _, after := f.counts(); after <= before {
		t.Fatal("stopping the agent must take the names setting off the computer, or it would point at a server it can no longer reach")
	}
}
