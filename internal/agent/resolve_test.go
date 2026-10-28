package agent

import (
	"context"
	"testing"
)

func TestAgentFollowsAChangedServerAddress(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.peers()[0].Endpoint; got != "203.0.113.10:51820" {
		t.Fatalf("%s", got)
	}

	// Nothing changed: looking again changes nothing.
	if d, err := r.a.ResolveNow(ctx); err != nil || !d.IsEmpty() {
		t.Fatalf("%+v %v", d, err)
	}

	// The name now points at a new server.
	r.setHost("vpn.example.com", "198.51.100.77")
	d, err := r.a.ResolveNow(ctx)
	if err != nil || len(d.Update) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	if got := r.peers()[0].Endpoint; got != "198.51.100.77:51820" {
		t.Fatalf("still using the old address: %s", got)
	}
	if d, _ := r.a.ResolveNow(ctx); !d.IsEmpty() {
		t.Fatal("applying the same address twice changed something")
	}
}

func TestAgentKeepsTheLastWorkingAddressWhenTheNameCannotBeFound(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.a.Sync(ctx)
	r.setHost("vpn.example.com") // the name now has no addresses
	if _, err := r.a.ResolveNow(ctx); err == nil {
		t.Fatal("expected an error")
	}
	if got := r.peers()[0].Endpoint; got != "203.0.113.10:51820" {
		t.Fatalf("the working address was thrown away: %s", got)
	}
	if r.a.problem() != ProblemNameNotFound {
		t.Fatalf("problem %q", r.a.problem())
	}
	// It recovers by itself.
	r.setHost("vpn.example.com", "203.0.113.10")
	if _, err := r.a.ResolveNow(ctx); err != nil || r.a.problem() != ProblemNone {
		t.Fatalf("%v %q", err, r.a.problem())
	}
}

func TestAnIPAddressNeedsNoLookup(t *testing.T) {
	r := newRig(t)
	r.ctl.set(func() { r.ctl.endpoint = "192.0.2.9:51820" })
	if _, err := r.a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.peers()[0].Endpoint; got != "192.0.2.9:51820" {
		t.Fatalf("%s", got)
	}
}

func TestANewServerKeyInTheInstructionsIsFollowedToo(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.a.Sync(ctx)
	old := r.ctl.serverPub
	_, np, _ := newKey(t)
	r.ctl.set(func() { r.ctl.serverPub = np; r.ctl.version++ })
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	p := r.peers()
	if len(p) != 1 || p[0].PublicKey != np || p[0].PublicKey == old {
		t.Fatalf("%+v", p)
	}
}
