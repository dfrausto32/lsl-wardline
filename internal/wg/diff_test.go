package wg

import (
	"context"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func peer(pub string, ips ...string) PeerConfig {
	p := PeerConfig{PublicKey: pub}
	for _, s := range ips {
		p.AllowedIPs = append(p.AllowedIPs, netip.MustParsePrefix(s))
	}
	return p
}

func live(p PeerConfig) PeerState { return PeerState{PeerConfig: p} }

func TestDiffEmpty(t *testing.T) {
	d := ComputeDiff(nil, nil)
	if !d.IsEmpty() {
		t.Fatalf("%+v", d)
	}
}

func TestDiffAddRemoveKeep(t *testing.T) {
	desired := []PeerConfig{peer("B", "10.0.0.3/32"), peer("A", "10.0.0.2/32")}
	liveSet := []PeerState{live(peer("A", "10.0.0.2/32")), live(peer("C", "10.0.0.9/32"))}
	d := ComputeDiff(desired, liveSet)
	if len(d.Add) != 1 || d.Add[0].PublicKey != "B" {
		t.Fatalf("add: %+v", d.Add)
	}
	if !reflect.DeepEqual(d.Remove, []string{"C"}) {
		t.Fatalf("remove: %+v", d.Remove)
	}
	if len(d.Update) != 0 {
		t.Fatalf("update: %+v", d.Update)
	}
}

func TestDiffDetectsChangedFields(t *testing.T) {
	base := peer("A", "10.0.0.2/32")
	cases := map[string]PeerConfig{
		"allowed ips": peer("A", "10.0.0.5/32"),
		"keepalive":   {PublicKey: "A", AllowedIPs: base.AllowedIPs, Keepalive: 25},
		"preshared":   {PublicKey: "A", AllowedIPs: base.AllowedIPs, PresharedKey: "PSK"},
		"endpoint":    {PublicKey: "A", AllowedIPs: base.AllowedIPs, Endpoint: "1.2.3.4:51820"},
	}
	for name, want := range cases {
		d := ComputeDiff([]PeerConfig{want}, []PeerState{live(base)})
		if len(d.Update) != 1 || len(d.Add) != 0 || len(d.Remove) != 0 {
			t.Errorf("%s: %+v", name, d)
		}
	}
}

func TestDiffIgnoresOrderAndObservedState(t *testing.T) {
	desired := []PeerConfig{peer("A", "10.0.0.2/32", "10.0.0.3/32")}
	l := live(peer("A", "10.0.0.3/32", "10.0.0.2/32"))
	l.Endpoint = "5.6.7.8:1234" // roaming peer: the server never configures an endpoint
	l.RxBytes = 99
	if d := ComputeDiff(desired, []PeerState{l}); !d.IsEmpty() {
		t.Fatalf("%+v", d)
	}
}

func TestDiffOutputIsSortedAndStable(t *testing.T) {
	desired := []PeerConfig{peer("Z", "10.0.0.9/32"), peer("M", "10.0.0.5/32"), peer("B", "10.0.0.2/32")}
	d := ComputeDiff(desired, nil)
	got := []string{d.Add[0].PublicKey, d.Add[1].PublicKey, d.Add[2].PublicKey}
	if !reflect.DeepEqual(got, []string{"B", "M", "Z"}) {
		t.Fatalf("%v", got)
	}
}

// Applying the same desired state twice must change nothing the second time.
func TestReconcileTwiceYieldsEmptyDiff(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	if err := f.Configure(ctx, "wg0", InterfaceConfig{PrivateKey: mustKey(t), ListenPort: 51820, Address: netip.MustParsePrefix("10.0.0.1/24")}); err != nil {
		t.Fatal(err)
	}
	desired := []PeerConfig{peer(mustPub(t), "10.0.0.2/32"), {PublicKey: mustPub(t), AllowedIPs: []netip.Prefix{netip.MustParsePrefix("10.0.0.3/32")}, Keepalive: 25}}
	d1, err := Reconcile(ctx, f, "wg0", desired)
	if err != nil || len(d1.Add) != 2 {
		t.Fatalf("first: %+v %v", d1, err)
	}
	d2, err := Reconcile(ctx, f, "wg0", desired)
	if err != nil || !d2.IsEmpty() {
		t.Fatalf("second apply must be a no-op: %+v %v", d2, err)
	}
	d3, err := Reconcile(ctx, f, "wg0", desired[:1])
	if err != nil || len(d3.Remove) != 1 {
		t.Fatalf("third: %+v %v", d3, err)
	}
}

func TestReconcileRejectsDuplicatePeers(t *testing.T) {
	f := NewFake()
	f.Configure(context.Background(), "wg0", InterfaceConfig{PrivateKey: mustKey(t), Address: netip.MustParsePrefix("10.0.0.1/24")})
	k := mustPub(t)
	_, err := Reconcile(context.Background(), f, "wg0", []PeerConfig{peer(k, "10.0.0.2/32"), peer(k, "10.0.0.3/32")})
	if err == nil || !strings.Contains(err.Error(), "share the public key") {
		t.Fatalf("duplicate public keys must be rejected, got %v", err)
	}
	_, err = Reconcile(context.Background(), f, "wg0", []PeerConfig{peer("nonsense", "10.0.0.2/32")})
	if err == nil {
		t.Fatal("malformed key must be rejected")
	}
}
