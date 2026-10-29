package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestControlServiceArgsCarryEverythingTheServiceNeeds(t *testing.T) {
	got := controlServiceArgs(":8080", `C:\ProgramData\Wardline-control`, "http://192.168.1.50:8080")
	want := []string{"control", "--listen", ":8080", "--data", `C:\ProgramData\Wardline-control`, "--public-url", "http://192.168.1.50:8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestGatewayServiceArgsNeverCarryTheOneTimeCode(t *testing.T) {
	got := gatewayServiceArgs("http://192.168.1.50:8080", `C:\ProgramData\Wardline-server`, "wardline0", "Office PC")
	want := []string{"gateway", "--control-url", "http://192.168.1.50:8080", "--state-dir", `C:\ProgramData\Wardline-server`, "--interface", "wardline0", "--name", "Office PC"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// Service settings can be read by every signed-in user, so the code is used once during install and never stored there.
	if strings.Contains(strings.Join(got, " "), "--token") {
		t.Fatal("the one-time code must not be part of the service command line")
	}
}

func TestFirewallRulesAreReplacedNotStacked(t *testing.T) {
	web := fwRule{Name: "Wardline web", Proto: "TCP", Port: "8080", Profile: "private,domain"}
	if got, want := firewallDeleteArgs(web.Name), []string{"advfirewall", "firewall", "delete", "rule", "name=Wardline web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	got := firewallAddArgs(web)
	want := []string{"advfirewall", "firewall", "add", "rule", "name=Wardline web", "dir=in", "action=allow", "protocol=TCP", "localport=8080", "profile=private,domain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestTheWebAppIsNotOpenedOnPublicNetworks(t *testing.T) {
	for _, r := range controlFirewall(":8080") {
		if strings.Contains(r.Profile, "public") {
			t.Fatalf("%s must not be open on public networks", r.Name)
		}
	}
	if len(controlFirewall(":8080")) != 1 || controlFirewall(":8080")[0].Port != "8080" {
		t.Fatal("expected one rule for the web port")
	}
	if g := gatewayFirewall(51820); len(g) != 1 || g[0].Proto != "UDP" || g[0].Port != "51820" {
		t.Fatalf("expected one UDP rule for the server port, got %+v", g)
	}
}

func TestDefaultPublicURLUsesThisComputersAddress(t *testing.T) {
	for in, want := range map[string]string{":8080": "http://192.168.1.50:8080", "0.0.0.0:9000": "http://192.168.1.50:9000"} {
		got, err := publicURLFor("192.168.1.50", in)
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v want %q", in, got, err, want)
		}
	}
	if _, err := publicURLFor("192.168.1.50", "nonsense"); err == nil {
		t.Error("a listen address without a port should be refused")
	}
}
