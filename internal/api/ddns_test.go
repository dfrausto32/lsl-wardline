package api

import (
	"bytes"
	"strings"
	"testing"
)

const duckToken = "d1f2a3b4-1111-2222-3333-444455556666"

func ddnsRig(t *testing.T) *harness {
	h := newHarness(t)
	h.setup()
	h.api.d.OnServerCreated = func(string) {} // this computer is the server
	return h
}

func TestSavingANameKeepsTheTokenSecret(t *testing.T) {
	h := ddnsRig(t)
	changed := 0
	h.api.d.DynamicDNSChanged = func() { changed++ }
	var out map[string]any
	h.mustCall("PUT", "/api/network/dynamic-dns", map[string]string{"name": "laney-network", "token": duckToken}, &out, 200)
	if out["full_name"] != "laney-network.duckdns.org" || changed != 1 {
		t.Fatalf("%v changed=%d", out, changed)
	}
	for _, v := range out {
		if s, _ := v.(string); strings.Contains(s, duckToken) {
			t.Fatal("the token came back in the reply")
		}
	}
	// Reading it back never shows the token either.
	h.mustCall("GET", "/api/network/dynamic-dns", nil, &out, 200)
	if out["configured"] != true || out["full_name"] != "laney-network.duckdns.org" {
		t.Fatalf("%v", out)
	}
	for _, v := range out {
		if s, _ := v.(string); strings.Contains(s, duckToken) {
			t.Fatal("the token came back when reading")
		}
	}
	// Stored encrypted, and in no log.
	var blob []byte
	if err := h.st.DB().QueryRow(`SELECT token_enc FROM dynamic_dns`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte(duckToken)) || len(blob) == 0 {
		t.Fatal("the token must be stored encrypted")
	}
	if strings.Contains(h.logs.String(), duckToken) {
		t.Fatal("the token appears in the logs")
	}
}

func TestTheChangeIsWrittenToActivityWithoutTheToken(t *testing.T) {
	h := ddnsRig(t)
	h.mustCall("PUT", "/api/network/dynamic-dns", map[string]string{"name": "laney-network", "token": duckToken}, nil, 200)
	var text string
	if err := h.st.DB().QueryRow(`SELECT plain_text FROM audit_events WHERE action='network.name_kept'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "laney-network.duckdns.org") || strings.Contains(text, duckToken) {
		t.Fatalf("activity says %q", text)
	}
}

func TestBadNamesAndTokensAreRefusedInPlainWords(t *testing.T) {
	h := ddnsRig(t)
	for _, in := range []map[string]string{
		{"name": "", "token": duckToken},
		{"name": "Has Space", "token": duckToken},
		{"name": "https://laney.duckdns.org", "token": duckToken},
		{"name": "laney.duckdns.org", "token": duckToken},
		{"name": "-bad", "token": duckToken},
		{"name": "laney-network", "token": ""},
		{"name": "laney-network", "token": "has space"},
	} {
		var e map[string]string
		h.mustCall("PUT", "/api/network/dynamic-dns", in, &e, 400)
		if e["error"] == "" || strings.Contains(e["error"], "sql") {
			t.Errorf("%v: %q", in, e["error"])
		}
	}
}

func TestTheNameCanBeRemoved(t *testing.T) {
	h := ddnsRig(t)
	h.mustCall("PUT", "/api/network/dynamic-dns", map[string]string{"name": "laney-network", "token": duckToken}, nil, 200)
	h.mustCall("DELETE", "/api/network/dynamic-dns", nil, nil, 200)
	var out map[string]any
	h.mustCall("GET", "/api/network/dynamic-dns", nil, &out, 200)
	if out["configured"] != false {
		t.Fatalf("%v", out)
	}
}

func TestOnlyWhenThisComputerIsTheServerAndSignedIn(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.mustCall("PUT", "/api/network/dynamic-dns", map[string]string{"name": "laney-network", "token": duckToken}, nil, 409)
	h.api.d.OnServerCreated = func(string) {}
	h.hc.Jar = nil
	h.mustCall("GET", "/api/network/dynamic-dns", nil, nil, 401)
}
