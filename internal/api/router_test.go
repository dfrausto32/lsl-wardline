package api

import (
	"context"
	"strings"
	"testing"

	"lsl-wardline/internal/portmap"
)

func routerRig(t *testing.T, r portmap.Result) *harness {
	h := newHarness(t)
	h.setup()
	h.api.d.OnServerCreated = func(string) {} // this computer is the server
	h.api.d.OpenRouterPort = func(context.Context) portmap.Result { return r }
	return h
}

func TestRouterForwardingWorked(t *testing.T) {
	h := routerRig(t, portmap.Result{Opened: true, RouterAddress: "203.0.113.7"})
	var out map[string]any
	h.mustCall("POST", "/api/network/router-forwarding", nil, &out, 200)
	if out["opened"] != true || !strings.Contains(out["message"].(string), "51820") {
		t.Fatalf("%v", out)
	}
}

func TestEveryFailureHasAPlainMessageAndPointsToTheManualSteps(t *testing.T) {
	for _, reason := range []portmap.Reason{portmap.NoRouter, portmap.Refused} {
		h := routerRig(t, portmap.Result{Reason: reason})
		var out map[string]any
		h.mustCall("POST", "/api/network/router-forwarding", nil, &out, 200)
		msg, _ := out["message"].(string)
		if out["opened"] != false || msg == "" || !strings.Contains(msg, "steps") {
			t.Errorf("%s: %v", reason, out)
		}
	}
}

func TestASharedProviderAddressIsExplained(t *testing.T) {
	h := routerRig(t, portmap.Result{Opened: true, Shared: true, RouterAddress: "100.72.1.9"})
	var out map[string]any
	h.mustCall("POST", "/api/network/router-forwarding", nil, &out, 200)
	if out["shared"] != true || !strings.Contains(out["message"].(string), "provider") {
		t.Fatalf("%v", out)
	}
}

func TestRouterForwardingIsOnlyOfferedWhenThisComputerIsTheServer(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.mustCall("POST", "/api/network/router-forwarding", nil, nil, 409)
}

func TestRouterForwardingNeedsASignedInAdmin(t *testing.T) {
	h := routerRig(t, portmap.Result{Opened: true})
	h.hc.Jar = nil
	h.mustCall("POST", "/api/network/router-forwarding", nil, nil, 401)
}
