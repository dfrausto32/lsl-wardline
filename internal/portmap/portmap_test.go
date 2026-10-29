package portmap

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type fakeGateway struct {
	external string
	failFor  map[uint32]bool // lease times the router refuses
	err      error
	added    []added
	deleted  int
}

type added struct {
	extPort, intPort uint16
	proto, client    string
	lease            uint32
}

func (f *fakeGateway) AddPortMapping(_ string, ext uint16, proto string, in uint16, client string, _ bool, _ string, lease uint32) error {
	if f.err != nil {
		return f.err
	}
	if f.failFor[lease] {
		return errors.New("OnlyPermanentLeasesSupported")
	}
	f.added = append(f.added, added{ext, in, proto, client, lease})
	return nil
}
func (f *fakeGateway) GetExternalIPAddress() (string, error)          { return f.external, nil }
func (f *fakeGateway) DeletePortMapping(string, uint16, string) error { f.deleted++; return nil }

func TestOpeningAPortSendsItToThisComputer(t *testing.T) {
	g := &fakeGateway{external: "203.0.113.7"}
	r := Open(context.Background(), []Gateway{g}, 51820, "192.168.1.50")
	if !r.Opened || r.RouterAddress != "203.0.113.7" || r.Reason != "" {
		t.Fatalf("%+v", r)
	}
	if len(g.added) != 1 || g.added[0] != (added{51820, 51820, "UDP", "192.168.1.50", 0}) {
		t.Fatalf("%+v", g.added)
	}
}

func TestARouterThatWantsATimeLimitGetsOne(t *testing.T) {
	g := &fakeGateway{external: "203.0.113.7", failFor: map[uint32]bool{0: true}}
	r := Open(context.Background(), []Gateway{g}, 51820, "192.168.1.50")
	if !r.Opened || len(g.added) != 1 || g.added[0].lease == 0 {
		t.Fatalf("%+v %+v", r, g.added)
	}
	if !r.NeedsRenewal {
		t.Fatal("a timed rule has to be renewed")
	}
}

func TestTheNextRouterServiceIsTriedWhenTheFirstRefuses(t *testing.T) {
	bad := &fakeGateway{err: errors.New("401 Invalid Action")}
	good := &fakeGateway{external: "203.0.113.7"}
	if r := Open(context.Background(), []Gateway{bad, good}, 51820, "192.168.1.50"); !r.Opened || len(good.added) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestNoRouterAnsweringIsReportedAsThat(t *testing.T) {
	if r := Open(context.Background(), nil, 51820, "192.168.1.50"); r.Opened || r.Reason != NoRouter {
		t.Fatalf("%+v", r)
	}
}

func TestARouterThatRefusesIsReportedAsThat(t *testing.T) {
	g := &fakeGateway{err: errors.New("718 ConflictInMappingEntry")}
	if r := Open(context.Background(), []Gateway{g}, 51820, "192.168.1.50"); r.Opened || r.Reason != Refused {
		t.Fatalf("%+v", r)
	}
}

func TestASharedProviderAddressIsFlagged(t *testing.T) {
	for _, ext := range []string{"10.20.30.40", "100.72.1.9", "192.168.0.2"} {
		g := &fakeGateway{external: ext}
		if r := Open(context.Background(), []Gateway{g}, 51820, "192.168.1.50"); !r.Opened || !r.Shared {
			t.Errorf("%s: %+v", ext, r)
		}
	}
	g := &fakeGateway{external: "203.0.113.7"}
	if r := Open(context.Background(), []Gateway{g}, 51820, "192.168.1.50"); r.Shared {
		t.Errorf("a public address is not shared: %+v", r)
	}
}

func TestIsPublic(t *testing.T) {
	for s, want := range map[string]bool{"203.0.113.7": true, "8.8.8.8": true, "10.0.0.1": false, "192.168.1.1": false, "172.16.0.1": false, "100.64.0.1": false, "127.0.0.1": false, "169.254.1.1": false} {
		if got := IsPublic(netip.MustParseAddr(s)); got != want {
			t.Errorf("%s: got %v want %v", s, got, want)
		}
	}
}
