package api

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/agent"
	"lsl-wardline/internal/gateway"
	"lsl-wardline/internal/wg"
)

// ---- helpers ----

type agentCreated struct {
	Device    map[string]any    `json:"device"`
	Token     string            `json:"token"`
	ExpiresAt time.Time         `json:"expires_at"`
	Install   map[string]string `json:"install"`
}

func (h *harness) addAgentDevice(personID, platform string) agentCreated {
	h.t.Helper()
	var c agentCreated
	h.mustCall("POST", "/api/devices", map[string]any{"person_id": personID, "platform": platform, "agent": true}, &c, 201)
	return c
}

// agentCall talks to the agent endpoints, which use a bearer token instead of the admin cookie.
func (h *harness) agentCall(method, path, token string, body any, out any) *http.Response {
	h.t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode == http.StatusOK {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res
}

type enrolled struct {
	pub  string
	priv string
	resp agent.EnrollResponse
}

// enrollAgent plays the part of the agent: it makes its own keypair and sends only the public half.
func (h *harness) enrollAgent(token string) enrolled {
	h.t.Helper()
	priv, pub, err := wg.GenerateKeypair()
	if err != nil {
		h.t.Fatal(err)
	}
	e := enrolled{pub: pub, priv: priv}
	h.mustAgent("POST", "/agent/v1/enroll", "", agent.EnrollRequest{Token: token, PublicKey: pub, AgentVersion: "test"}, &e.resp, 200)
	return e
}

func (h *harness) mustAgent(method, path, token string, body any, out any, want int) {
	h.t.Helper()
	res := h.agentCall(method, path, token, body, out)
	if res.StatusCode != want {
		h.t.Fatalf("%s %s: status %d, want %d", method, path, res.StatusCode, want)
	}
}

// ---- tests ----

func TestAgentDeviceStartsWaitingWithoutAKeyAndStoresOnlyAHashOfTheCode(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")

	c := h.addAgentDevice(sam, "linux")
	if c.Device["kind"] != "agent" || c.Device["status"] != "waiting" || c.Device["platform"] != "linux" {
		t.Fatalf("%v", c.Device)
	}
	if c.Token == "" || !c.ExpiresAt.After(h.now().Add(23*time.Hour)) {
		t.Fatalf("token %q expires %v", c.Token, c.ExpiresAt)
	}
	if !strings.Contains(c.Install["command"], c.Token) {
		t.Fatalf("the install command should carry the code: %v", c.Install)
	}

	id := c.Device["id"].(string)
	var keys int
	h.st.DB().QueryRow(`SELECT count(*) FROM device_keys WHERE device_id=?`, id).Scan(&keys)
	if keys != 0 {
		t.Fatal("no key may exist before the device sends its own public key")
	}
	var leaks int
	h.st.DB().QueryRow(`SELECT count(*) FROM enrollment_tokens WHERE token_hash LIKE ?`, "%"+c.Token+"%").Scan(&leaks)
	if leaks != 0 {
		t.Fatal("the code must be stored as a hash only")
	}

	// Only Windows and Linux computers get one-click setup.
	var e map[string]string
	h.mustCall("POST", "/api/devices", map[string]any{"person_id": sam, "platform": "ios", "agent": true}, &e, 400)
	if e["error"] == "" {
		t.Fatal("expected a plain-language message")
	}
}

func TestAgentEnrollmentWorksOnceExpiresAndTakesOnlyAPublicKey(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	c := h.addAgentDevice(sam, "windows")
	id := c.Device["id"].(string)

	_, pub, _ := wg.GenerateKeypair()
	h.mustAgent("POST", "/agent/v1/enroll", "", agent.EnrollRequest{Token: "wrong", PublicKey: pub}, nil, 401)
	h.mustAgent("POST", "/agent/v1/enroll", "", agent.EnrollRequest{Token: c.Token, PublicKey: "not a key"}, nil, 400)

	e := h.enrollAgent(c.Token)
	if e.resp.DeviceID != id || e.resp.AuthToken == "" || len(e.resp.ControlKey) != ed25519.PublicKeySize {
		t.Fatalf("%+v", e.resp)
	}
	var stored, version string
	h.st.DB().QueryRow(`SELECT public_key FROM device_keys WHERE device_id=? AND state='active'`, id).Scan(&stored)
	h.st.DB().QueryRow(`SELECT agent_version FROM devices WHERE id=?`, id).Scan(&version)
	if stored != e.pub || version != "test" {
		t.Fatalf("stored key %q, version %q", stored, version)
	}

	// Single use.
	h.mustAgent("POST", "/agent/v1/enroll", "", agent.EnrollRequest{Token: c.Token, PublicKey: pub}, nil, 401)

	// Activity says it in plain words, and does not contain the code.
	var found bool
	var recs []map[string]any
	h.mustCall("GET", "/api/activity", nil, &recs, 200)
	for _, r := range recs {
		text := r["text"].(string)
		if strings.Contains(text, c.Token) {
			t.Fatal("the code is in the activity log")
		}
		if strings.Contains(text, "Sam's Windows computer") && strings.Contains(text, "set up") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no plain sentence for enrollment: %v", recs)
	}

	// A code expires after 24 hours.
	c2 := h.addAgentDevice(sam, "linux")
	h.advance(25 * time.Hour)
	h.mustAgent("POST", "/agent/v1/enroll", "", agent.EnrollRequest{Token: c2.Token, PublicKey: pub}, nil, 401)
}

func TestAnUnusedCodeStopsWorkingWhenTheDeviceIsRemoved(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	c := h.addAgentDevice(h.person("Sam"), "linux")
	h.mustCall("DELETE", "/api/devices/"+c.Device["id"].(string), nil, nil, 200)
	_, pub, _ := wg.GenerateKeypair()
	h.mustAgent("POST", "/agent/v1/enroll", "", agent.EnrollRequest{Token: c.Token, PublicKey: pub}, nil, 401)
}

func TestAgentGetsSignedInstructionsAndLosesAccessWhenRemoved(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	c := h.addAgentDevice(h.person("Sam"), "linux")
	e := h.enrollAgent(c.Token)

	h.mustAgent("GET", "/agent/v1/desired?since=-1", "", nil, nil, 401)
	h.mustAgent("GET", "/agent/v1/desired?since=-1", "wrong", nil, nil, 401)

	var signed gateway.Signed
	h.mustAgent("GET", "/agent/v1/desired?since=-1", e.resp.AuthToken, nil, &signed, 200)
	st, err := agent.Verify(e.resp.ControlKey, signed)
	if err != nil {
		t.Fatal(err)
	}
	var serverPub string
	h.st.DB().QueryRow(`SELECT public_key FROM servers WHERE role='active'`).Scan(&serverPub)
	var addr, rng string
	h.st.DB().QueryRow(`SELECT tunnel_ip FROM devices WHERE id=?`, e.resp.DeviceID).Scan(&addr)
	h.st.DB().QueryRow(`SELECT address_range FROM networks`).Scan(&rng)
	switch {
	case st.DeviceID != e.resp.DeviceID:
		t.Errorf("device id %q", st.DeviceID)
	case st.ServerPublicKey != serverPub:
		t.Errorf("server key %q", st.ServerPublicKey)
	case st.Endpoint != "vpn.example.com:51820":
		t.Errorf("endpoint %q", st.Endpoint)
	case !strings.HasPrefix(st.Address, addr+"/"):
		t.Errorf("address %q, device has %q", st.Address, addr)
	case len(st.AllowedIPs) != 1 || st.AllowedIPs[0] != rng:
		t.Errorf("allowed %v, network %q", st.AllowedIPs, rng)
	case st.PresharedKey == "":
		t.Error("no preshared key")
	case st.Keepalive != 25:
		t.Errorf("keepalive %d", st.Keepalive)
	}
	if bad := signed; len(bad.Payload) > 0 {
		bad.Payload = append([]byte(nil), bad.Payload...)
		bad.Payload[len(bad.Payload)/2] ^= 1
		if _, err := agent.Verify(e.resp.ControlKey, bad); err == nil {
			t.Error("a tampered payload was accepted")
		}
	}

	// Removing the device ends its access to instructions as well as to the server.
	h.mustCall("DELETE", "/api/devices/"+e.resp.DeviceID, nil, nil, 200)
	h.mustAgent("GET", "/agent/v1/desired?since=-1", e.resp.AuthToken, nil, nil, 401)
}

func TestServerAcceptsAnEnrolledAgentWithTheSamePresharedKey(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	ctx := context.Background()
	c := h.addAgentDevice(h.person("Sam"), "linux")

	// Not enrolled yet: there is no key, so the server has nothing to allow.
	g.Sync(ctx)
	if p, _ := fake.Peers(ctx, "wardline0"); len(p) != 0 {
		t.Fatalf("the server allowed a device that has not sent a key: %v", p)
	}

	e := h.enrollAgent(c.Token)
	g.Sync(ctx)
	peers, _ := fake.Peers(ctx, "wardline0")
	if len(peers) != 1 || peers[0].PublicKey != e.pub {
		t.Fatalf("server peers: %+v", peers)
	}
	var signed gateway.Signed
	h.mustAgent("GET", "/agent/v1/desired?since=-1", e.resp.AuthToken, nil, &signed, 200)
	st, _ := agent.Verify(e.resp.ControlKey, signed)
	if st.PresharedKey == "" || peers[0].PresharedKey != st.PresharedKey {
		t.Fatal("the server and the device must hold the same preshared key")
	}
}

func TestAgentLongPollWaitsThenWakesOnChange(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	e := h.enrollAgent(h.addAgentDevice(sam, "linux").Token)

	var first gateway.Signed
	h.mustAgent("GET", "/agent/v1/desired?since=-1", e.resp.AuthToken, nil, &first, 200)
	st, _ := agent.Verify(e.resp.ControlKey, first)

	// Nothing changed: the request waits, then says "no news".
	start := time.Now()
	h.mustAgent("GET", "/agent/v1/desired?since="+itoa(st.Version), e.resp.AuthToken, nil, nil, 204)
	if time.Since(start) < 200*time.Millisecond {
		t.Fatal("the request did not wait")
	}

	// A change anywhere in the network wakes it up straight away.
	got := make(chan int64, 1)
	go func() {
		var s gateway.Signed
		res := h.agentCall("GET", "/agent/v1/desired?since="+itoa(st.Version), e.resp.AuthToken, nil, &s)
		if res.StatusCode != 200 {
			got <- -1
			return
		}
		next, _ := agent.Verify(e.resp.ControlKey, s)
		got <- next.Version
	}()
	time.Sleep(50 * time.Millisecond)
	h.addDevice(sam, "android")
	select {
	case v := <-got:
		if v <= st.Version {
			t.Fatalf("version %d did not advance past %d", v, st.Version)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent was not woken by a change")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
