package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestLinuxCommandsPointJustTheNetworksNamesAtTheServer(t *testing.T) {
	cmds, err := resolvectlApply("wardline-agent", "wardline.internal", "10.77.5.1")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"resolvectl", "dns", "wardline-agent", "10.77.5.1"},
		{"resolvectl", "domain", "wardline-agent", "wardline.internal"},
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Fatalf("%v", cmds)
	}
	// A "~" in front would make the domain routing-only; without it, short names work too, which is wanted.
	if strings.Contains(strings.Join(cmds[1], " "), "~") {
		t.Fatal("short names should work, so the domain must not be routing-only")
	}
	if got := resolvectlClear("wardline-agent"); !reflect.DeepEqual(got, []string{"resolvectl", "revert", "wardline-agent"}) {
		t.Fatalf("%v", got)
	}
}

func TestWhatTheControlPlaneSendsIsCheckedBeforeItReachesAnyCommand(t *testing.T) {
	bad := []struct{ iface, domain, server string }{
		{"wardline-agent", "wardline.internal", "not-an-ip"},
		{"wardline-agent", "wardline.internal", "10.0.0.1; rm -rf /"},
		{"wardline-agent", "wardline.internal'; Stop-Computer; '", "10.77.5.1"},
		{"wardline-agent", "$(reboot).internal", "10.77.5.1"},
		{"wardline-agent", "", "10.77.5.1"},
		{"wardline-agent", "wardline.internal", "::1"}, // the server is reached over IPv4 only
		{"-rf", "wardline.internal", "10.77.5.1"},
		{"a b", "wardline.internal", "10.77.5.1"},
	}
	for _, b := range bad {
		if _, err := resolvectlApply(b.iface, b.domain, b.server); err == nil {
			t.Errorf("resolvectl accepted %+v", b)
		}
		if _, err := nrptApplyScript(b.domain, b.server); err == nil && b.iface == "wardline-agent" {
			t.Errorf("the Windows script accepted %+v", b)
		}
	}
}

func TestWindowsScriptAddsOneRuleForTheEndingAndRemovesOnlyOurs(t *testing.T) {
	s, err := nrptApplyScript("wardline.internal", "10.77.5.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"Add-DnsClientNrptRule", "'.wardline.internal'", "'10.77.5.1'", "-Comment 'Wardline'", "Remove-DnsClientNrptRule"} {
		if !strings.Contains(s, must) {
			t.Errorf("script lacks %s:\n%s", must, s)
		}
	}
	// Only rules this program made (marked with the comment) are ever removed.
	if !strings.Contains(s, "$_.Comment -eq 'Wardline'") {
		t.Errorf("removal must be limited to Wardline's own rules:\n%s", s)
	}
	c := nrptClearScript()
	if !strings.Contains(c, "$_.Comment -eq 'Wardline'") || strings.Contains(c, "Add-DnsClientNrptRule") {
		t.Errorf("clearing must only remove Wardline's rules:\n%s", c)
	}
}
