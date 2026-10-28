package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/auth"
	"lsl-wardline/internal/gateway"
	wlog "lsl-wardline/internal/log"
	"lsl-wardline/internal/store"
	"lsl-wardline/internal/wg"
)

const goodPassword = "a long enough passphrase"

type harness struct {
	t      *testing.T
	ts     *httptest.Server
	hc     *http.Client
	st     *store.Store
	api    *Server
	logs   *bytes.Buffer
	dbPath string
	box    *auth.SecretBox

	mu     sync.Mutex
	offset time.Duration
}

func (h *harness) now() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return time.Now().Add(h.offset)
}

func (h *harness) advance(d time.Duration) {
	h.mu.Lock()
	h.offset += d
	h.mu.Unlock()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, logs: &bytes.Buffer{}}
	h.dbPath = filepath.Join(t.TempDir(), "c.db")
	st, err := store.Open(h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h.st = st
	key := bytes.Repeat([]byte{42}, 32)
	h.box, _ = auth.NewSecretBox(key)
	sk, err := EnsureSigningKey(context.Background(), st, h.box)
	if err != nil {
		t.Fatal(err)
	}
	h.api = New(Deps{
		Store: st, Audit: audit.New(st.DB()), Box: h.box, Log: wlog.New(h.logs, "debug"),
		SigningKey: sk, PublicURL: "https://control.example", Now: h.now, HoldTime: 300 * time.Millisecond,
	})
	h.ts = httptest.NewServer(h.api.Handler())
	t.Cleanup(h.ts.Close)
	jar, _ := cookiejar.New(nil)
	h.hc = &http.Client{Jar: jar}
	return h
}

// call sends JSON and decodes the JSON reply into out (if not nil).
func (h *harness) call(method, path string, body any, out any) *http.Response {
	h.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-Wardline", "1")
	}
	res, err := h.hc.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res
}

func (h *harness) mustCall(method, path string, body any, out any, want int) {
	h.t.Helper()
	res := h.call(method, path, body, out)
	if res.StatusCode != want {
		h.t.Fatalf("%s %s: status %d, want %d (%v)", method, path, res.StatusCode, want, out)
	}
}

func (h *harness) setup() {
	h.t.Helper()
	h.mustCall("POST", "/api/setup", map[string]string{"network_name": "Home", "email": "maria@example.com", "password": goodPassword}, nil, 201)
}

var tokenRe = regexp.MustCompile(`WARDLINE_ENROLL_TOKEN=(\S+)`)

// connectedServer sets everything up through the API and returns a running-ready gateway.
func (h *harness) connectedServer(dir string) (*gateway.Gateway, *wg.Fake) {
	h.t.Helper()
	h.setup()
	h.mustCall("PUT", "/api/network/server-address", map[string]string{"address": "vpn.example.com"}, nil, 200)
	var created struct {
		Install map[string]string `json:"install"`
	}
	h.mustCall("POST", "/api/servers", map[string]string{"name": "Office server"}, &created, 201)
	m := tokenRe.FindStringSubmatch(created.Install["docker"])
	if m == nil {
		h.t.Fatalf("no token in install command: %s", created.Install["docker"])
	}
	fake := wg.NewFake()
	g := gateway.New(gateway.Config{
		StateDir: dir, Iface: "wardline0", Name: "Office server", EnrollToken: m[1], ControlURL: h.ts.URL,
		Control: gateway.NewHTTPControl(h.ts.URL, h.ts.Client()), Backend: fake, Log: wlog.New(h.logs, "debug"),
		ReportEvery: 50 * time.Millisecond,
	})
	if err := g.Start(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	return g, fake
}

type addedDevice struct {
	Device   map[string]any `json:"device"`
	Config   string         `json:"config"`
	QR       string         `json:"qr_png_base64"`
	Filename string         `json:"filename"`
}

func (h *harness) person(name string) string {
	h.t.Helper()
	var p struct {
		ID string `json:"id"`
	}
	h.mustCall("POST", "/api/people", map[string]string{"name": name}, &p, 201)
	return p.ID
}

func (h *harness) addDevice(personID, platform string) addedDevice {
	h.t.Helper()
	var a addedDevice
	h.mustCall("POST", "/api/devices", map[string]string{"person_id": personID, "platform": platform}, &a, 201)
	return a
}

// login signs in again, for tests that move the clock past the session lifetime.
func (h *harness) login() {
	h.t.Helper()
	h.mustCall("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": goodPassword}, nil, 200)
}
