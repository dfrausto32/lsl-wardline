package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestFindingThePublicAddress(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.api.d.PublicIP = func(context.Context) (string, error) { return "203.0.113.7", nil }
	var out map[string]string
	h.mustCall("GET", "/api/network/public-address", nil, &out, 200)
	if out["address"] != "203.0.113.7" {
		t.Fatalf("%v", out)
	}
}

func TestAFailedLookupIsExplainedInPlainWords(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.api.d.PublicIP = func(context.Context) (string, error) { return "", errors.New("dial tcp: i/o timeout") }
	var out map[string]string
	h.mustCall("GET", "/api/network/public-address", nil, &out, 502)
	if out["error"] == "" || out["error"] == "dial tcp: i/o timeout" {
		t.Fatalf("expected a plain message, got %q", out["error"])
	}
}

func TestOnlyASignedInAdminCanLookItUp(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.api.d.PublicIP = func(context.Context) (string, error) { return "203.0.113.7", nil }
	h.hc = &http.Client{} // no session
	h.mustCall("GET", "/api/network/public-address", nil, nil, 401)
}

func TestLookedUpAddressesMustBePublic(t *testing.T) {
	for _, bad := range []string{"", "not an ip", "10.0.0.5", "192.168.1.9", "127.0.0.1", "100.64.1.2", "169.254.1.1"} {
		if _, err := checkPublicAddress(bad); err == nil {
			t.Errorf("%q should not be accepted as a public address", bad)
		}
	}
	if got, err := checkPublicAddress("203.0.113.7\n"); err != nil || got != "203.0.113.7" {
		t.Errorf("got %q, %v", got, err)
	}
}
