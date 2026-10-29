package wg

import (
	"strings"
	"testing"
)

func TestTheFirewallRulesOnlyRelayInsideTheTunnel(t *testing.T) {
	rules, err := nftRules("wardline0", true)
	if err != nil {
		t.Fatal(err)
	}
	// Forwarding between devices is allowed, but nothing may be relayed into or out of the tunnel
	// to the rest of the server's network, such as the internet or a cloud provider's inside network.
	for _, want := range []string{`iifname "wardline0" oifname != "wardline0" drop`, `oifname "wardline0" iifname != "wardline0" drop`, "delete table inet wardline_forward"} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules are missing %q:\n%s", want, rules)
		}
	}
}

func TestTurningItOffBlocksAllRelayingThroughTheTunnel(t *testing.T) {
	// Isolation must not depend on the host's own switch: a host that already relays (one running containers, for
	// example) would otherwise let devices reach each other with the setting off.
	rules, err := nftRules("wardline0", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`iifname "wardline0" drop`, `oifname "wardline0" drop`, "delete table inet wardline_forward"} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules are missing %q:\n%s", want, rules)
		}
	}
	if strings.Contains(strings.ReplaceAll(rules, "policy accept", ""), "accept") { // only the chain's default may say accept
		t.Fatalf("nothing may be accepted when relaying is off:\n%s", rules)
	}
}

func TestInterfaceNamesAreCheckedBeforeTheyReachAShellOrAFirewall(t *testing.T) {
	for _, bad := range []string{"", "a b", `x"; drop`, "../../etc", "toolonginterfacename1", "wardline0\n"} {
		if _, err := nftRules(bad, true); err == nil {
			t.Errorf("%q should be refused", bad)
		}
		if _, err := forwardingPath(bad); err == nil {
			t.Errorf("%q should be refused for the switch file too", bad)
		}
	}
	if p, err := forwardingPath("wardline0"); err != nil || p != "/proc/sys/net/ipv4/conf/wardline0/forwarding" {
		t.Fatalf("%q %v", p, err)
	}
}

func TestWindowsForwardingCommand(t *testing.T) {
	got := netshForwardingArgs("wardline0", true)
	want := []string{"interface", "ipv4", "set", "interface", "interface=wardline0", "forwarding=enabled"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %q", got)
	}
	if g := netshForwardingArgs("wardline0", false); g[len(g)-1] != "forwarding=disabled" {
		t.Fatalf("got %q", g)
	}
}
