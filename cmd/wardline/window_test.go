package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/agent"
)

type windowRig struct {
	status agent.Status
	acts   []string
	opened []string
	quit   bool
	srv    *httptest.Server
}

func newWindowRig(t *testing.T) *windowRig {
	r := &windowRig{status: agent.Status{
		Time: time.Now(), Connected: true, Address: "10.113.18.2/24", Server: "network.example.com:51820",
		ControlURL: "https://network.example.com", Version: "v0.1.8", RxBytes: 1536, TxBytes: 3 << 20,
		LastHandshake: time.Now().Add(-40 * time.Second),
	}}
	d := windowDeps{
		status: func() agent.Status { return r.status },
		now:    time.Now,
		act:    func(name string) error { r.acts = append(r.acts, name); return nil },
		open:   func(url string) { r.opened = append(r.opened, url) },
		quit:   func() { r.quit = true },
	}
	r.srv = httptest.NewServer(windowHandler(testKey, d))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *windowRig) do(t *testing.T, method, path, key, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("X-Wardline-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestTheWindowPageNeedsTheKey(t *testing.T) {
	r := newWindowRig(t)
	for url, want := range map[string]int{"/": 403, "/?k=nope": 403, "/?k=" + testKey: 200} {
		resp, err := http.Get(r.srv.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: %d want %d", url, resp.StatusCode, want)
		}
	}
}

func TestTheStatusIsInPlainWords(t *testing.T) {
	r := newWindowRig(t)
	code, out := r.do(t, "GET", "/api/status", testKey, "")
	if code != 200 {
		t.Fatalf("%d", code)
	}
	want := map[string]string{"state": "connected", "headline": "Connected", "address": "10.113.18.2", "server": "network.example.com", "sent": "3.0 MB", "received": "1.5 KB", "version": "v0.1.8", "web_url": "https://network.example.com"}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("%s = %v, want %v", k, out[k], v)
		}
	}
	if s, _ := out["last_connected"].(string); !strings.Contains(s, "seconds ago") {
		t.Errorf("last_connected = %q", s)
	}
}

func TestTheStatusNeedsTheKeyToo(t *testing.T) {
	r := newWindowRig(t)
	if code, _ := r.do(t, "GET", "/api/status", "", ""); code != 403 {
		t.Fatalf("%d", code)
	}
}

func TestWhenTheServiceStoppedTheWindowSaysSo(t *testing.T) {
	r := newWindowRig(t)
	r.status.Time = time.Now().Add(-5 * time.Minute)
	_, out := r.do(t, "GET", "/api/status", testKey, "")
	if out["state"] != "off" || !strings.Contains(out["headline"].(string), "not running") {
		t.Fatalf("%v", out)
	}
}

func TestOnlyKnownActionsAreAccepted(t *testing.T) {
	r := newWindowRig(t)
	for _, name := range []string{"disconnect", "connect", "refresh", "remove"} {
		if code, out := r.do(t, "POST", "/api/action", testKey, `{"name":"`+name+`"}`); code != 200 || out["ok"] != true {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if len(r.acts) != 4 {
		t.Fatalf("acts %v", r.acts)
	}
	before := len(r.acts)
	for _, bad := range []string{"format-disk", "", "../remove", "REMOVE"} {
		if code, _ := r.do(t, "POST", "/api/action", testKey, `{"name":"`+bad+`"}`); code != 400 {
			t.Errorf("%q: %d", bad, code)
		}
	}
	if len(r.acts) != before {
		t.Fatal("an unknown action ran")
	}
	if code, _ := r.do(t, "POST", "/api/action", "", `{"name":"remove"}`); code != 403 || len(r.acts) != before {
		t.Fatal("an action without the key must not run")
	}
}

func TestOpeningTheWebAppUsesTheNetworksOwnAddress(t *testing.T) {
	r := newWindowRig(t)
	r.do(t, "POST", "/api/action", testKey, `{"name":"web"}`)
	if len(r.opened) != 1 || r.opened[0] != "https://network.example.com" {
		t.Fatalf("opened %v", r.opened)
	}
	r.status.ControlURL = "javascript:alert(1)"
	r.do(t, "POST", "/api/action", testKey, `{"name":"web"}`)
	if len(r.opened) != 1 {
		t.Fatalf("only web addresses may be opened: %v", r.opened)
	}
}

func TestCloseQuits(t *testing.T) {
	r := newWindowRig(t)
	r.do(t, "POST", "/api/action", testKey, `{"name":"close"}`)
	if !r.quit {
		t.Fatal("close should end the window")
	}
}

func TestHumanNumbers(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1536: "1.5 KB", 3 << 20: "3.0 MB", 5 << 30: "5.0 GB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("%d: %q want %q", n, got, want)
		}
	}
	now := time.Now()
	for d, want := range map[time.Duration]string{5 * time.Second: "5 seconds ago", time.Minute: "1 minute ago", 90 * time.Minute: "1 hour ago", 50 * time.Hour: "2 days ago"} {
		if got := ago(now.Add(-d), now); got != want {
			t.Errorf("%v: %q want %q", d, got, want)
		}
	}
	if ago(time.Time{}, now) != "not yet" {
		t.Error("never connected")
	}
}
