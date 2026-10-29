package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu   sync.Mutex
	runs []gatewayOpts
	done chan struct{}
}

func newRecorder() *recorder { return &recorder{done: make(chan struct{}, 4)} }

func (r *recorder) run(_ context.Context, o *gatewayOpts) error {
	r.mu.Lock()
	r.runs = append(r.runs, *o)
	r.mu.Unlock()
	r.done <- struct{}{}
	return nil
}

func (r *recorder) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the server was not started")
	}
}

func TestTheLocalServerEnrollsOverTheLoopbackWithTheNewCode(t *testing.T) {
	r := newRecorder()
	l := &localServer{ctx: context.Background(), listen: ":8080", stateDir: t.TempDir(), name: "Office PC", run: r.run}
	l.start("code-1")
	r.wait(t)
	got := r.runs[0]
	if got.controlURL != "http://127.0.0.1:8080" || got.token != "code-1" || got.name != "Office PC" || got.stateDir != l.stateDir {
		t.Fatalf("unexpected options: %+v", got)
	}
}

func TestTheLocalServerIsOnlyStartedOnce(t *testing.T) {
	r := newRecorder()
	l := &localServer{ctx: context.Background(), listen: "127.0.0.1:9000", stateDir: t.TempDir(), run: r.run}
	l.start("a")
	l.start("b")
	r.wait(t)
	time.Sleep(50 * time.Millisecond)
	if len(r.runs) != 1 || r.runs[0].token != "a" || r.runs[0].controlURL != "http://127.0.0.1:9000" {
		t.Fatalf("runs: %+v", r.runs)
	}
}

func TestAnAlreadyEnrolledLocalServerComesBackAtBoot(t *testing.T) {
	dir := t.TempDir()
	r := newRecorder()
	l := &localServer{ctx: context.Background(), listen: ":8080", stateDir: dir, run: r.run}
	if l.startIfEnrolled() {
		t.Fatal("nothing is enrolled yet, so nothing should start")
	}
	if err := os.WriteFile(filepath.Join(dir, "identity.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !l.startIfEnrolled() {
		t.Fatal("an enrolled server should start by itself")
	}
	r.wait(t)
	if r.runs[0].token != "" {
		t.Fatal("a server that is already enrolled has no use for a code")
	}
}

func TestLocalServerServiceArgsAndFirewall(t *testing.T) {
	if got, want := localServerArgs(`C:\ProgramData\Wardline-server`), []string{"--with-server", "--server-dir", `C:\ProgramData\Wardline-server`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
