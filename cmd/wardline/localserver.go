package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"

	wlog "lsl-wardline/internal/log"
)

// localServer is the server role running inside the control plane's own process. It is for the
// one-computer case: when the web app creates the server, this registers it straight away, so nobody
// copies a command. The server role itself is the same code the separate server install runs.
type localServer struct {
	ctx      context.Context
	listen   string // the control plane's listen address, to reach it over the loopback
	stateDir string
	name     string
	level    string
	run      func(ctx context.Context, o *gatewayOpts) error // replaced in tests

	mu      sync.Mutex
	started bool
}

func localServerArgs(dir string) []string {
	return []string{"--with-server", "--server-dir", dir}
}

func (l *localServer) loopbackURL() string {
	_, port, err := net.SplitHostPort(l.listen)
	if err != nil {
		port = "8080"
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port)
}

// start runs the server role with the one-time code. It returns at once and only ever starts one.
func (l *localServer) start(code string) {
	l.mu.Lock()
	if l.started {
		l.mu.Unlock()
		return
	}
	l.started = true
	l.mu.Unlock()
	o := &gatewayOpts{controlURL: l.loopbackURL(), token: code, stateDir: l.stateDir, iface: "wardline0", name: l.name, backend: "auto", level: l.level}
	run := l.run
	if run == nil {
		run = runGateway
	}
	go func() {
		if err := run(l.ctx, o); err != nil && !errors.Is(err, context.Canceled) {
			wlog.New(os.Stderr, l.level).Error("the server on this computer stopped", "error", err.Error())
		}
	}()
}

// startIfEnrolled brings back a server that was registered before, when the control plane restarts.
func (l *localServer) startIfEnrolled() bool {
	if _, err := os.Stat(filepath.Join(l.stateDir, "identity.json")); err != nil {
		return false
	}
	l.start("")
	return true
}

func runGateway(ctx context.Context, o *gatewayOpts) error {
	g, err := buildGateway(o)
	if err != nil {
		return err
	}
	return g.Run(ctx)
}
