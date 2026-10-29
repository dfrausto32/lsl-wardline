package api

import "testing"

func TestTheServerAddressIsSuggestedFromTheAddressTheWebAppIsReachedAt(t *testing.T) {
	h := newHarness(t)
	h.setup()
	for public, want := range map[string]string{
		"http://192.168.1.50:8080":     "192.168.1.50",
		"https://wardline.example.org": "wardline.example.org",
		"http://localhost:8080":        "",
		"http://127.0.0.1:8080":        "",
		"not a url":                    "",
	} {
		h.api.d.PublicURL = public
		var out map[string]any
		h.mustCall("GET", "/api/network", nil, &out, 200)
		if got, _ := out["suggested_address"].(string); got != want {
			t.Errorf("%s: suggested %q, want %q", public, got, want)
		}
	}
}

func TestTheAddressScreenIsToldWhenThisComputerIsTheServer(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var out map[string]any
	h.mustCall("GET", "/api/network", nil, &out, 200)
	if out["local_server"] != false {
		t.Fatalf("no local server is configured: %v", out["local_server"])
	}
	h.api.d.OnServerCreated = func(string) {}
	h.mustCall("GET", "/api/network", nil, &out, 200)
	if out["local_server"] != true {
		t.Fatalf("the screen should know this computer is the server: %v", out["local_server"])
	}
}
