package main

import "testing"

func TestTheNameServiceGetsItsOwnFirewallRule(t *testing.T) {
	r := namesFirewall()
	if len(r) != 1 || r[0].Proto != "UDP" || r[0].Port != "53" || r[0].Name != namesRuleName {
		t.Fatalf("%+v", r)
	}
	if namesRuleName == serverRuleName || namesRuleName == webRuleName {
		t.Fatal("the rules must have different names or removing one would remove another")
	}
}
