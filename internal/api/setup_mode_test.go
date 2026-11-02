package api

import "testing"

func TestAManagedGroupIsTheDefaultAndAnOpenOneIsNotAvailableYet(t *testing.T) {
	h := newHarness(t)
	var e map[string]string
	h.mustCall("POST", "/api/setup", map[string]string{"network_name": "Home", "email": "maria@example.com", "password": goodPassword, "group_mode": "open"}, &e, 400)
	if e["error"] == "" || !containsFold(e["error"], "not available yet") {
		t.Fatalf("an open group must be refused in plain words, got %q", e["error"])
	}
	// Nothing was created by the refused attempt.
	var n int
	h.st.DB().QueryRow(`SELECT count(*) FROM networks`).Scan(&n)
	if n != 0 {
		t.Fatal("a refused setup left a network behind")
	}
	h.mustCall("POST", "/api/setup", map[string]string{"network_name": "Home", "email": "maria@example.com", "password": goodPassword, "group_mode": "managed"}, nil, 201)
	var net map[string]any
	h.mustCall("GET", "/api/network", nil, &net, 200)
	if net["group_mode"] != "managed" {
		t.Fatalf("network says %v", net["group_mode"])
	}
	var mode string
	h.st.DB().QueryRow(`SELECT mode FROM groups`).Scan(&mode)
	if mode != "authority" {
		t.Fatalf("the group row says %q", mode)
	}
}

func TestLeavingTheModeOutStillMakesAManagedGroup(t *testing.T) {
	h := newHarness(t)
	h.setup() // the usual call sends no group_mode
	var net map[string]any
	h.mustCall("GET", "/api/network", nil, &net, 200)
	if net["group_mode"] != "managed" {
		t.Fatalf("%v", net["group_mode"])
	}
}

func containsFold(s, sub string) bool {
	ls, lsub := []rune(s), []rune(sub)
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			if lower(ls[i+j]) != lower(lsub[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
