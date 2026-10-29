package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lsl-wardline/internal/portmap"
)

func TestTheRouterIsNotTouchedUntilThePersonAskedForIt(t *testing.T) {
	var calls atomic.Int32
	k := &routerKeeper{dir: t.TempDir(), every: 10 * time.Millisecond, open: func(context.Context) portmap.Result { calls.Add(1); return portmap.Result{Opened: true} }}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	k.run(ctx)
	if calls.Load() != 0 {
		t.Fatalf("the router was asked %d times without the person choosing it", calls.Load())
	}
}

func TestAChosenRouterRuleIsRenewedAtStartAndThenRegularly(t *testing.T) {
	var calls atomic.Int32
	k := &routerKeeper{dir: t.TempDir(), every: 15 * time.Millisecond, open: func(context.Context) portmap.Result { calls.Add(1); return portmap.Result{Opened: true} }}
	k.remember()
	if _, err := os.Stat(filepath.Join(k.dir, "router-forwarding.on")); err != nil {
		t.Fatal("remember should leave a marker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	k.run(ctx)
	if calls.Load() < 3 {
		t.Fatalf("expected a call at start and renewals, got %d", calls.Load())
	}
}
