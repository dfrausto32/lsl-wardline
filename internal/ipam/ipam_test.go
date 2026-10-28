package ipam

import (
	"context"
	"errors"
	"math/rand"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"lsl-wardline/internal/store"
)

func setup(t *testing.T, prefix string) (*Allocator, *store.Store, *time.Time) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mustExec(t, s, `INSERT INTO networks(id,name,address_range,server_address,server_address_kind,created_at) VALUES('n1','Home',?,'vpn.example.com','name','2026-01-01T00:00:00Z')`, prefix)
	mustExec(t, s, `INSERT INTO people(id,network_id,display_name,created_at) VALUES('p1','n1','Sam','2026-01-01T00:00:00Z')`)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	a := &Allocator{DB: s.DB(), Now: func() time.Time { return now }}
	if err := a.Init(context.Background(), s.DB(), "n1", netip.MustParsePrefix(prefix)); err != nil {
		t.Fatal(err)
	}
	return a, s, &now
}

func mustExec(t *testing.T, s *store.Store, q string, args ...any) {
	t.Helper()
	if _, err := s.DB().Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func addDevice(t *testing.T, s *store.Store, id string) {
	t.Helper()
	// tunnel_ip is filled in by the caller after allocation in real code; use a unique placeholder here.
	mustExec(t, s, `INSERT INTO devices(id,network_id,person_id,name,platform,kind,tunnel_ip,status,added_at) VALUES(?,'n1','p1',?,'ios','manual',?,'waiting','2026-01-01T00:00:00Z')`,
		id, "dev-"+id, "placeholder-"+id)
}

func TestChooseRangeAvoidsCommonAndGivenRanges(t *testing.T) {
	avoid := []netip.Prefix{netip.MustParsePrefix("10.100.0.0/16")}
	for seed := int64(0); seed < 200; seed++ {
		p, err := ChooseRange(avoid, rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatal(err)
		}
		if p.Bits() != 24 || !p.Addr().IsPrivate() {
			t.Fatalf("not a private /24: %v", p)
		}
		for _, bad := range append(CommonRanges(), avoid...) {
			if bad.Overlaps(p) {
				t.Fatalf("seed %d: %v overlaps %v", seed, p, bad)
			}
		}
	}
}

func TestChooseRangeIsDeterministicForASeed(t *testing.T) {
	a, _ := ChooseRange(nil, rand.New(rand.NewSource(7)))
	b, _ := ChooseRange(nil, rand.New(rand.NewSource(7)))
	if a != b {
		t.Fatalf("%v != %v", a, b)
	}
}

func TestChooseRangeFailsClearlyWhenNothingIsFree(t *testing.T) {
	_, err := ChooseRange([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, rand.New(rand.NewSource(1)))
	if !errors.Is(err, ErrNoRange) {
		t.Fatalf("got %v", err)
	}
}

func TestServerTakesTheFirstUsableAddress(t *testing.T) {
	a, s, _ := setup(t, "10.77.5.0/24")
	got, err := a.ServerIP(context.Background(), s.DB(), "n1")
	if err != nil || got.String() != "10.77.5.1" {
		t.Fatalf("server ip = %v, %v", got, err)
	}
}

func TestDevicesGetLowestFreeAddress(t *testing.T) {
	a, s, _ := setup(t, "10.77.5.0/24")
	ctx := context.Background()
	var got []string
	for _, id := range []string{"d1", "d2", "d3"} {
		addDevice(t, s, id)
		ip, err := a.Allocate(ctx, s.DB(), "n1", id)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ip.String())
	}
	want := []string{"10.77.5.2", "10.77.5.3", "10.77.5.4"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestAllocateIsIdempotentPerDevice(t *testing.T) {
	a, s, _ := setup(t, "10.77.5.0/24")
	addDevice(t, s, "d1")
	x, _ := a.Allocate(context.Background(), s.DB(), "n1", "d1")
	y, err := a.Allocate(context.Background(), s.DB(), "n1", "d1")
	if err != nil || x != y {
		t.Fatalf("%v %v %v", x, y, err)
	}
}

func TestRemovedAddressIsQuarantinedForSevenDays(t *testing.T) {
	a, s, now := setup(t, "10.77.5.0/24")
	ctx := context.Background()
	addDevice(t, s, "d1")
	addDevice(t, s, "d2")
	ip1, _ := a.Allocate(ctx, s.DB(), "n1", "d1") // .2
	if err := a.Release(ctx, s.DB(), "n1", "d1"); err != nil {
		t.Fatal(err)
	}
	ip2, _ := a.Allocate(ctx, s.DB(), "n1", "d2")
	if ip2 == ip1 {
		t.Fatalf("address %v reused during quarantine", ip1)
	}

	addDevice(t, s, "d3")
	*now = now.Add(7*24*time.Hour - time.Minute)
	ip3, _ := a.Allocate(ctx, s.DB(), "n1", "d3")
	if ip3 == ip1 {
		t.Fatal("reused one minute before quarantine ends")
	}

	addDevice(t, s, "d4")
	*now = now.Add(2 * time.Minute)
	ip4, err := a.Allocate(ctx, s.DB(), "n1", "d4")
	if err != nil || ip4 != ip1 {
		t.Fatalf("after 7 days expected %v back, got %v (%v)", ip1, ip4, err)
	}
}

func TestReleaseOfUnknownDeviceIsAnError(t *testing.T) {
	a, _, _ := setup(t, "10.77.5.0/24")
	if err := a.Release(context.Background(), a.DB, "n1", "ghost"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNetworkAndBroadcastAddressesAreNeverHandedOut(t *testing.T) {
	a, s, _ := setup(t, "10.77.5.0/29") // .0 network, .7 broadcast, .1 server, .2-.6 devices
	ctx := context.Background()
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		addDevice(t, s, id)
		ip, err := a.Allocate(ctx, s.DB(), "n1", id)
		if err != nil {
			t.Fatalf("device %d: %v", i, err)
		}
		seen[ip.String()] = true
	}
	if seen["10.77.5.0"] || seen["10.77.5.7"] || seen["10.77.5.1"] {
		t.Fatalf("reserved address handed out: %v", seen)
	}
	addDevice(t, s, "z")
	if _, err := a.Allocate(ctx, s.DB(), "n1", "z"); !errors.Is(err, ErrFull) {
		t.Fatalf("want ErrFull, got %v", err)
	}
}

func TestAllocationRollsBackWithTheCallersTransaction(t *testing.T) {
	a, s, _ := setup(t, "10.77.5.0/24")
	addDevice(t, s, "d1")
	tx, _ := s.DB().Begin()
	ip, err := a.Allocate(context.Background(), tx, "n1", "d1")
	if err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	again, _ := a.Allocate(context.Background(), s.DB(), "n1", "d1")
	if again != ip {
		t.Fatalf("after rollback want %v again, got %v", ip, again)
	}
}
