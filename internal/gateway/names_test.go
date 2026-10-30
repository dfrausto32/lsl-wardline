package gateway

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"lsl-wardline/internal/dnsserver"
)

// fakeListener stands in for binding the server's own address on port 53, which a test cannot do.
type fakeListener struct {
	mu    sync.Mutex
	conns []net.PacketConn
	addrs []netip.Addr
	fail  error
}

func (f *fakeListener) listen(a netip.Addr) (net.PacketConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f.conns = append(f.conns, pc)
	f.addrs = append(f.addrs, a)
	return pc, nil
}

func (f *fakeListener) last() (net.PacketConn, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.conns) == 0 {
		return nil, 0
	}
	return f.conns[len(f.conns)-1], len(f.conns)
}

func ask(t *testing.T, pc net.PacketConn, name string) ([]netip.Addr, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, rcode, err := dnsserver.Query(ctx, pc.LocalAddr().String(), name)
	if err != nil {
		t.Fatal(err)
	}
	return addrs, rcode
}

func withNames(version int64, recs ...NameRecord) DesiredState {
	st := state(version)
	st.DevicesReachEachOther = true
	st.NamesDomain = "wardline.internal"
	st.Names = recs
	return st
}

func TestTheServerAnswersNamesFromTheInstructionsItGets(t *testing.T) {
	c := newFakeControl(t)
	g, _, _ := newGateway(t, c, t.TempDir())
	f := &fakeListener{}
	g.cfg.ListenNames = f.listen
	ctx := context.Background()
	t.Cleanup(g.stopNames)

	c.set(t, withNames(1, NameRecord{"office-pc", "10.77.5.2"}, NameRecord{"server", "10.77.5.1"}))
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	pc, n := f.last()
	if pc == nil || n != 1 {
		t.Fatalf("the name service should be running on the server's own address (%d listeners)", n)
	}
	if f.addrs[0] != netip.MustParseAddr("10.77.5.1") {
		t.Fatalf("it must listen on the server's own network address, not everywhere: %v", f.addrs[0])
	}
	if a, rc := ask(t, pc, "office-pc.wardline.internal"); rc != 0 || len(a) != 1 || a[0] != netip.MustParseAddr("10.77.5.2") {
		t.Fatalf("%v %d", a, rc)
	}

	// A rename arrives as a new state: the same service answers the new names without restarting.
	c.set(t, withNames(2, NameRecord{"back-pc", "10.77.5.2"}, NameRecord{"server", "10.77.5.1"}))
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, n := f.last(); n != 1 {
		t.Fatalf("a rename must not restart the service (%d listeners)", n)
	}
	if _, rc := ask(t, pc, "office-pc.wardline.internal"); rc != dnsserver.RcodeNameError {
		t.Fatalf("the old name should be gone, rcode %d", rc)
	}
	if a, rc := ask(t, pc, "back-pc.wardline.internal"); rc != 0 || len(a) != 1 {
		t.Fatalf("%v %d", a, rc)
	}
}

func TestTurningNamesOffStopsTheService(t *testing.T) {
	c := newFakeControl(t)
	g, _, _ := newGateway(t, c, t.TempDir())
	f := &fakeListener{}
	g.cfg.ListenNames = f.listen
	ctx := context.Background()
	t.Cleanup(g.stopNames)

	c.set(t, withNames(1, NameRecord{"server", "10.77.5.1"}))
	g.Sync(ctx)
	pc, _ := f.last()
	if pc == nil {
		t.Fatal("not started")
	}
	c.set(t, state(2)) // reaching each other is off again: no names
	g.Sync(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		qctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		_, _, err := dnsserver.Query(qctx, pc.LocalAddr().String(), "server.wardline.internal")
		cancel()
		if err != nil {
			break // nobody answers any more
		}
		if time.Now().After(deadline) {
			t.Fatal("the name service is still answering after names were turned off")
		}
	}
}

func TestAServiceThatCannotStartIsTriedAgainOnTheNextInstructions(t *testing.T) {
	c := newFakeControl(t)
	g, _, logs := newGateway(t, c, t.TempDir())
	f := &fakeListener{fail: errors.New("address in use")}
	g.cfg.ListenNames = f.listen
	ctx := context.Background()
	t.Cleanup(g.stopNames)

	c.set(t, withNames(1, NameRecord{"server", "10.77.5.1"}))
	if _, err := g.Sync(ctx); err != nil {
		t.Fatalf("names failing must not stop the tunnel from being set up: %v", err)
	}
	if !contains(logs.String(), "name service") {
		t.Fatalf("the failure should be logged: %s", logs.String())
	}
	f.mu.Lock()
	f.fail = nil
	f.mu.Unlock()
	c.set(t, withNames(2, NameRecord{"server", "10.77.5.1"}))
	g.Sync(ctx)
	if pc, _ := f.last(); pc == nil {
		t.Fatal("it should have started on the next instructions")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
