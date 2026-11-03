package wg

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

// Reach-all permission (task 6.9): specs/BUSINESS-RULES.md.

func addrs(t *testing.T, ss ...string) []netip.Addr {
	t.Helper()
	var out []netip.Addr
	for _, s := range ss {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
}

func TestRelayingForEverybodyAndForNobodyAreTheRulesThatAlreadyExist(t *testing.T) {
	for _, all := range []bool{true, false} {
		want, _ := nftRules("wardline0", all)
		got, err := nftRulesFor("wardline0", RelayPolicy{All: all})
		if err != nil || got != want {
			t.Fatalf("all=%v: rules changed (err %v):\n%s\nwant\n%s", all, err, got, want)
		}
	}
	// Everybody wins over the list: with everyone allowed the list adds nothing.
	want, _ := nftRules("wardline0", true)
	got, _ := nftRulesFor("wardline0", RelayPolicy{All: true, ReachAll: addrs(t, "10.1.0.5")})
	if got != want {
		t.Fatalf("when every machine may reach every other, the list adds nothing:\n%s", got)
	}
}

func TestOnlyTrafficToOrFromAPermittedMachineIsRelayedBetweenDevices(t *testing.T) {
	rules, err := nftRulesFor("wardline0", RelayPolicy{ReachAll: addrs(t, "10.1.0.9", "10.1.0.5", "10.1.0.5")})
	if err != nil {
		t.Fatal(err)
	}
	// Still nothing to or from the rest of the server's network, as when relaying is on for everybody.
	for _, want := range []string{
		`iifname "wardline0" oifname != "wardline0" drop`,
		`oifname "wardline0" iifname != "wardline0" drop`,
		"delete table inet wardline_forward",
		// Two ways: from a permitted machine to anyone, and from anyone to a permitted machine. The list is sorted and has no repeats.
		`iifname "wardline0" oifname "wardline0" ip saddr { 10.1.0.5, 10.1.0.9 } accept`,
		`iifname "wardline0" oifname "wardline0" ip daddr { 10.1.0.5, 10.1.0.9 } accept`,
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules are missing %q:\n%s", want, rules)
		}
	}
	// Everything else between devices ends in a drop, and the drop comes after both accepts.
	drop := strings.LastIndex(rules, `iifname "wardline0" oifname "wardline0" drop`)
	if drop < 0 || drop < strings.LastIndex(rules, "accept\n") {
		t.Fatalf("devices that hold no permission must be dropped after the accepts:\n%s", rules)
	}
	// The only accepts are the two for the permitted machines (the chain's own default is the only other "accept").
	if n := strings.Count(strings.ReplaceAll(rules, "policy accept", ""), "accept"); n != 2 {
		t.Fatalf("want exactly two accept rules, got %d:\n%s", n, rules)
	}
}

func TestOnlyIPv4AddressesGoIntoTheRules(t *testing.T) {
	for _, bad := range []string{"fd00::1", "::ffff:10.1.0.5"} {
		if _, err := nftRulesFor("wardline0", RelayPolicy{ReachAll: addrs(t, bad)}); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if _, err := nftRulesFor("wardline0", RelayPolicy{ReachAll: []netip.Addr{{}}}); err == nil {
		t.Error("an empty address should be refused")
	}
	if _, err := nftRulesFor("a b", RelayPolicy{ReachAll: addrs(t, "10.1.0.5")}); err == nil {
		t.Error("a bad interface name should still be refused")
	}
}

func TestThePretendServerRemembersWhoMayReachWhom(t *testing.T) {
	f := NewFake()
	ctx := context.Background()
	if _, set := f.Relay("wardline0"); set {
		t.Fatal("nothing has been set yet")
	}
	if err := f.SetRelay(ctx, "wardline0", RelayPolicy{ReachAll: addrs(t, "10.1.0.9", "10.1.0.5")}); err != nil {
		t.Fatal(err)
	}
	p, set := f.Relay("wardline0")
	if !set || p.All || len(p.ReachAll) != 2 || p.ReachAll[0].String() != "10.1.0.5" {
		t.Fatalf("%+v set=%v", p, set)
	}
	// The older on/off call still works, and means everybody or nobody.
	if err := f.SetForwarding(ctx, "wardline0", true); err != nil {
		t.Fatal(err)
	}
	if p, _ := f.Relay("wardline0"); !p.All || len(p.ReachAll) != 0 {
		t.Fatalf("%+v", p)
	}
	if on, _ := f.Forwarding("wardline0"); !on {
		t.Fatal("relaying for everybody is on")
	}
	f.SetRelay(ctx, "wardline0", RelayPolicy{ReachAll: addrs(t, "10.1.0.5")})
	if on, _ := f.Forwarding("wardline0"); !on {
		t.Fatal("relaying for a permitted machine means relaying is on")
	}
}
