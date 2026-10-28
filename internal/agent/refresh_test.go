package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/wg"
)

// connectOnNewKey makes the fake server accept the new key: the tunnel handshakes once the agent switches to it.
func (r *rig) connectOnNewKey() {
	r.ctl.onRefresh = func(newPub string) {
		for i := 0; i < 200; i++ {
			cfg, ok := r.fake.Interface("wardline-agent")
			if ok {
				if pub, err := wg.PublicFromPrivate(cfg.PrivateKey); err == nil && pub == newPub {
					r.fake.SimulateHandshake("wardline-agent", r.ctl.serverPub, 1, 1, "203.0.113.10:51820")
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestRefreshSwitchesToANewKeyAndForgetsTheOldOne(t *testing.T) {
	r := newRig(t)
	r.connectOnNewKey()
	ctx := context.Background()
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	old := r.a.identity()
	if err := r.a.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	now := r.a.identity()
	if now.PublicKey == old.PublicKey || now.PrivateKey == old.PrivateKey {
		t.Fatal("the key did not change")
	}
	if now.PreviousPrivateKey != "" || now.PendingPrivateKey != "" {
		t.Fatalf("old or pending private keys were kept: %+v", now)
	}
	cfg, _ := r.fake.Interface("wardline-agent")
	if cfg.PrivateKey != now.PrivateKey {
		t.Fatal("the tunnel is not using the new key")
	}
	if len(r.ctl.refreshed) != 1 || r.ctl.refreshed[0] != now.PublicKey {
		t.Fatalf("control plane was told %v", r.ctl.refreshed)
	}
	for _, b := range r.ctl.sent {
		if strings.Contains(string(b), now.PrivateKey) || strings.Contains(string(b), old.PrivateKey) {
			t.Fatal("a private key was sent to the control plane")
		}
	}
	// The next instructions agree, and applying them changes nothing.
	if d, err := r.a.Sync(ctx); err != nil || !d.IsEmpty() {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestRefreshThatTheControlPlaneRefusesLeavesTheOldKeyWorking(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.a.Sync(ctx)
	old := r.a.identity()
	r.ctl.set(func() { r.ctl.refreshErr = errors.New("the control plane is down") })
	if err := r.a.Refresh(ctx); err == nil {
		t.Fatal("expected an error")
	}
	now := r.a.identity()
	cfg, _ := r.fake.Interface("wardline-agent")
	if now.PublicKey != old.PublicKey || cfg.PrivateKey != old.PrivateKey || now.PendingPrivateKey != "" {
		t.Fatalf("the old key must keep working and no pending key may be left: %+v", now)
	}
	if r.a.problem() != ProblemRefreshFailed {
		t.Fatalf("problem %q", r.a.problem())
	}
}

func TestRefreshThatNeverConnectsRollsBackToTheOldKey(t *testing.T) {
	r := newRig(t) // no handshake ever happens on the new key
	ctx := context.Background()
	r.a.Sync(ctx)
	old := r.a.identity()
	err := r.a.Refresh(ctx)
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("%v", err)
	}
	now := r.a.identity()
	cfg, _ := r.fake.Interface("wardline-agent")
	if now.PublicKey != old.PublicKey || cfg.PrivateKey != old.PrivateKey {
		t.Fatalf("the agent is not back on its old key: %+v", now)
	}
	if r.ctl.rollbacks != 1 {
		t.Fatalf("the control plane was told %d times", r.ctl.rollbacks)
	}
	if now.PreviousPrivateKey != "" || now.PendingPrivateKey != "" {
		t.Fatalf("leftover keys: %+v", now)
	}
	if r.a.problem() != ProblemRefreshFailed {
		t.Fatalf("problem %q", r.a.problem())
	}
}

func TestAgentThatCrashedMidRefreshFollowsWhatTheControlPlaneAccepts(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.a.Sync(ctx)

	// The agent saved a new key and asked, then died before switching.
	priv, pub, _ := wg.GenerateKeypair()
	r.a.mu.Lock()
	r.a.id.PendingPrivateKey, r.a.id.PendingPublicKey = priv, pub
	r.a.mu.Unlock()
	r.ctl.set(func() { r.ctl.previous, r.ctl.current = r.ctl.current, pub; r.ctl.version++ })
	if _, err := r.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.a.identity(); got.PublicKey != pub || got.PrivateKey != priv || got.PendingPrivateKey != "" {
		t.Fatalf("the agent did not move to the key the control plane accepts: %+v", got)
	}

	// Another agent that asked but was never accepted drops its pending key instead.
	r2 := newRig(t)
	r2.a.Sync(ctx)
	keep := r2.a.identity().PublicKey
	p2, k2, _ := wg.GenerateKeypair()
	r2.a.mu.Lock()
	r2.a.id.PendingPrivateKey, r2.a.id.PendingPublicKey = p2, k2
	r2.a.mu.Unlock()
	r2.ctl.set(func() { r2.ctl.version++ })
	if _, err := r2.a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r2.a.identity(); got.PendingPrivateKey != "" || got.PublicKey != keep {
		t.Fatalf("pending key kept or key changed: %+v", got)
	}
}

func TestAgentRefusesToConnectWithAKeyItDoesNotHold(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.a.Sync(ctx)
	_, stranger, _ := wg.GenerateKeypair()
	r.ctl.set(func() { r.ctl.current = stranger; r.ctl.version++ })
	if _, err := r.a.Sync(ctx); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("%v", err)
	}
	if r.a.problem() != ProblemKeyMismatch {
		t.Fatalf("problem %q", r.a.problem())
	}
}

func TestRefreshStartsOnItsOwnWhenTheControlPlaneSaysItIsDue(t *testing.T) {
	r := newRig(t)
	r.connectOnNewKey()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.a.Sync(ctx)
	old := r.a.identity().PublicKey
	due := time.Now().Add(-time.Hour)
	r.ctl.set(func() { r.ctl.refreshDue = &due; r.ctl.version++ })
	r.a.Sync(ctx)
	if err := r.a.RefreshIfDue(ctx); err != nil {
		t.Fatal(err)
	}
	if r.a.identity().PublicKey == old {
		t.Fatal("no refresh happened although it was due")
	}
	// Not due: nothing happens.
	later := time.Now().Add(24 * time.Hour)
	r.ctl.set(func() { r.ctl.refreshDue = &later; r.ctl.version++ })
	r.a.Sync(ctx)
	before := r.a.identity().PublicKey
	r.a.RefreshIfDue(ctx)
	if r.a.identity().PublicKey != before {
		t.Fatal("refreshed before it was due")
	}
}

func TestRefreshCanBeAskedForByHandAndHappensOnce(t *testing.T) {
	r := newRig(t)
	r.connectOnNewKey()
	ctx := context.Background()
	r.a.Sync(ctx)
	before := r.a.identity().PublicKey

	if err := r.a.RefreshIfRequested(ctx); err != nil || r.a.identity().PublicKey != before {
		t.Fatalf("nothing was asked for, yet: %v", err)
	}
	if err := RequestRefresh(r.dir); err != nil {
		t.Fatal(err)
	}
	if err := r.a.RefreshIfRequested(ctx); err != nil {
		t.Fatal(err)
	}
	after := r.a.identity().PublicKey
	if after == before {
		t.Fatal("no refresh happened")
	}
	// The request is used up.
	if err := r.a.RefreshIfRequested(ctx); err != nil || r.a.identity().PublicKey != after {
		t.Fatalf("it refreshed again by itself: %v", err)
	}
}
