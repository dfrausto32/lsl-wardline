package api

import (
	"context"
	"strings"
	"testing"
)

func TestDevicesAreIsolatedUntilThePersonTurnsOnReaching(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	ctx := context.Background()
	var net map[string]any
	h.mustCall("GET", "/api/network", nil, &net, 200)
	if net["devices_reach_each_other"] != false {
		t.Fatalf("devices must be isolated by default: %v", net["devices_reach_each_other"])
	}
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if on, set := fake.Forwarding("wardline0"); !set || on {
		t.Fatalf("the server must be told forwarding is off (set=%v on=%v)", set, on)
	}

	h.mustCall("PUT", "/api/network/devices-reach-each-other", map[string]bool{"enabled": true}, &net, 200)
	if net["devices_reach_each_other"] != true {
		t.Fatalf("%v", net)
	}
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if on, _ := fake.Forwarding("wardline0"); !on {
		t.Fatal("the server should now relay between devices")
	}

	h.mustCall("PUT", "/api/network/devices-reach-each-other", map[string]bool{"enabled": false}, &net, 200)
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if on, _ := fake.Forwarding("wardline0"); on {
		t.Fatal("turning it off must stop the relaying")
	}
}

func TestChangingItIsWrittenToActivityInPlainWords(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.mustCall("PUT", "/api/network/devices-reach-each-other", map[string]bool{"enabled": true}, nil, 200)
	var text string
	if err := h.st.DB().QueryRow(`SELECT plain_text FROM audit_events WHERE action='network.reach_changed'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "reach each other") || !strings.Contains(text, "on") {
		t.Fatalf("activity says %q", text)
	}
}

func TestOnlyASignedInAdminCanChangeIt(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.hc.Jar = nil
	h.mustCall("PUT", "/api/network/devices-reach-each-other", map[string]bool{"enabled": true}, nil, 401)
}
