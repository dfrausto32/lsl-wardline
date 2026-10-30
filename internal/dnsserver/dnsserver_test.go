package dnsserver

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func records() map[string]netip.Addr {
	return map[string]netip.Addr{
		"office-pc": netip.MustParseAddr("10.77.5.2"),
		"files":     netip.MustParseAddr("10.77.5.2"),
		"server":    netip.MustParseAddr("10.77.5.1"),
	}
}

func handler() *Handler {
	return &Handler{Zone: "wardline.internal", Lookup: func(l string) (netip.Addr, bool) { a, ok := records()[l]; return a, ok }}
}

func TestKnownNameGetsItsAddress(t *testing.T) {
	r, err := ParseResponse(handler().Answer(BuildQuery(7, "Office-PC.wardline.internal.", TypeA)))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != 7 || r.Rcode != RcodeOK || len(r.Addrs) != 1 || r.Addrs[0] != netip.MustParseAddr("10.77.5.2") {
		t.Fatalf("%+v", r)
	}
	if !r.Authoritative || !r.Response {
		t.Fatalf("flags: %+v", r)
	}
}

func TestOtherRecordTypesOfAKnownNameAreEmptyNotMissing(t *testing.T) {
	for _, typ := range []uint16{TypeAAAA, 16 /* TXT */, 15 /* MX */} {
		r, err := ParseResponse(handler().Answer(BuildQuery(1, "office-pc.wardline.internal", typ)))
		if err != nil || r.Rcode != RcodeOK || len(r.Addrs) != 0 {
			t.Fatalf("type %d: %+v %v", typ, r, err)
		}
	}
}

func TestUnknownNameUnderTheEndingIsNoSuchName(t *testing.T) {
	for _, name := range []string{"nobody.wardline.internal", "a.b.wardline.internal", "wardline.internal"} {
		r, err := ParseResponse(handler().Answer(BuildQuery(1, name, TypeA)))
		if err != nil || r.Rcode != RcodeNameError {
			t.Fatalf("%s: %+v %v", name, r, err)
		}
	}
}

func TestOutsideTheEndingIsRefusedNeverForwarded(t *testing.T) {
	for _, name := range []string{"example.com", "office-pc", "evilwardline.internal"} {
		r, err := ParseResponse(handler().Answer(BuildQuery(1, name, TypeA)))
		if err != nil || r.Rcode != RcodeRefused {
			t.Fatalf("%s: %+v %v", name, r, err)
		}
	}
}

func TestGarbageIsIgnored(t *testing.T) {
	h := handler()
	pointer := append(BuildQuery(1, "x.wardline.internal", TypeA)[:12], 0xC0, 0x0C, 0, 1, 0, 1)
	for _, junk := range [][]byte{nil, {1, 2, 3}, make([]byte, 11), pointer} {
		if got := h.Answer(junk); got != nil {
			t.Fatalf("answered junk %x with %x", junk, got)
		}
	}
	// A response sent to us (the top bit of the flags set) is never answered.
	q := BuildQuery(1, "office-pc.wardline.internal", TypeA)
	q[2] |= 0x80
	if h.Answer(q) != nil {
		t.Fatal("answered a response")
	}
}

func TestServeAnswersOverUDPAndStopsWhenAsked(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Serve(ctx, pc, handler()); close(done) }()

	qctx, qcancel := context.WithTimeout(ctx, 3*time.Second)
	defer qcancel()
	addrs, rcode, err := Query(qctx, pc.LocalAddr().String(), "server.wardline.internal")
	if err != nil || rcode != RcodeOK || len(addrs) != 1 || addrs[0] != netip.MustParseAddr("10.77.5.1") {
		t.Fatalf("%v %v %v", addrs, rcode, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not stop")
	}
}
