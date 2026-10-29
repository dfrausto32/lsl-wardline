package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"lsl-wardline/internal/portmap"
)

// routerKeeper remembers that the person chose to let Wardline open the router's port, and asks the
// router again at start and every few hours, because routers forget such rules when they restart or
// when the rule has a time limit. It does nothing unless the person chose it.
type routerKeeper struct {
	dir   string
	every time.Duration
	open  func(ctx context.Context) portmap.Result
}

func (k *routerKeeper) marker() string { return filepath.Join(k.dir, "router-forwarding.on") }

func (k *routerKeeper) remember() {
	if err := os.MkdirAll(k.dir, 0o700); err == nil {
		_ = os.WriteFile(k.marker(), []byte("on\n"), 0o600)
	}
}

// run blocks until ctx ends.
func (k *routerKeeper) run(ctx context.Context) {
	if _, err := os.Stat(k.marker()); err != nil {
		return
	}
	k.open(ctx)
	t := time.NewTicker(k.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.open(ctx)
		}
	}
}

// openRouterPort asks the router to send the server port to this computer.
func openRouterPort(ctx context.Context) portmap.Result {
	ip, err := outboundIP()
	if err != nil {
		return portmap.Result{Reason: portmap.NoRouter}
	}
	return portmap.Open(ctx, portmap.Discover(ctx), gatewayPort, ip)
}
