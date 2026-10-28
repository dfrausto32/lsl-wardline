package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/auth"
)

// ---- first run and sign-in ----

func TestStateBeforeAndAfterSetup(t *testing.T) {
	h := newHarness(t)
	var st map[string]any
	h.mustCall("GET", "/api/state", nil, &st, 200)
	if st["setup_done"] != false || st["signed_in"] != false {
		t.Fatalf("%v", st)
	}
	h.setup()
	h.mustCall("GET", "/api/state", nil, &st, 200)
	if st["setup_done"] != true || st["signed_in"] != true || st["network_name"] != "Home" {
		t.Fatalf("%v", st)
	}
}

func TestSetupHappensOnlyOnceAndChecksInput(t *testing.T) {
	h := newHarness(t)
	var e map[string]string
	h.mustCall("POST", "/api/setup", map[string]string{"network_name": "Home", "email": "m@example.com", "password": "short"}, &e, 400)
	if !strings.Contains(e["error"], "10 characters") {
		t.Fatalf("%v", e)
	}
	h.mustCall("POST", "/api/setup", map[string]string{"network_name": "  ", "email": "m@example.com", "password": goodPassword}, nil, 400)
	h.setup()
	h.mustCall("POST", "/api/setup", map[string]string{"network_name": "Again", "email": "x@example.com", "password": goodPassword}, &e, 409)
}

func TestNothingIsReachableWithoutSigningIn(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.mustCall("DELETE", "/api/session", nil, nil, 200)
	for _, p := range []string{"/api/home", "/api/devices", "/api/people", "/api/activity", "/api/activity.csv", "/api/servers", "/api/network"} {
		if res := h.call("GET", p, nil, nil); res.StatusCode != 401 {
			t.Errorf("GET %s without a session: %d", p, res.StatusCode)
		}
	}
	if res := h.call("POST", "/api/devices", map[string]string{}, nil); res.StatusCode != 401 {
		t.Errorf("POST devices: %d", res.StatusCode)
	}
}

func TestStateChangingRequestsNeedTheSafetyHeader(t *testing.T) {
	h := newHarness(t)
	h.setup()
	req, _ := http.NewRequest("POST", h.ts.URL+"/api/people", strings.NewReader(`{"name":"Sam"}`))
	req.Header.Set("Content-Type", "application/json")
	res, _ := h.hc.Do(req)
	if res.StatusCode != 403 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestLoginCookieAndWrongPassword(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.mustCall("DELETE", "/api/session", nil, nil, 200)

	var e map[string]string
	h.mustCall("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": "nope nope nope"}, &e, 401)
	if strings.Contains(strings.ToLower(e["error"]), "password is wrong") || e["error"] == "" {
		t.Fatalf("message: %v", e)
	}

	res := h.call("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": goodPassword}, nil)
	if res.StatusCode != 200 {
		t.Fatalf("%d", res.StatusCode)
	}
	var found bool
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			found = true
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("cookie flags: %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("no session cookie")
	}
	h.mustCall("GET", "/api/home", nil, nil, 200)
	h.mustCall("DELETE", "/api/session", nil, nil, 200)
	h.mustCall("GET", "/api/home", nil, nil, 401)
}

func TestSignInIsRateLimited(t *testing.T) {
	h := newHarness(t)
	h.setup()
	h.mustCall("DELETE", "/api/session", nil, nil, 200)
	bad := map[string]string{"email": "maria@example.com", "password": "wrong wrong wrong"}
	for i := 0; i < 5; i++ {
		h.mustCall("POST", "/api/session", bad, nil, 401)
	}
	var e map[string]string
	h.mustCall("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": goodPassword}, &e, 429)
	if !strings.Contains(e["error"], "minutes") {
		t.Fatalf("%v", e)
	}
	h.advance(16 * time.Minute)
	h.mustCall("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": goodPassword}, nil, 200)
}

func TestAuthenticatorAppFlow(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var begin struct{ Secret, URL string }
	h.mustCall("POST", "/api/totp/begin", nil, &begin, 200)
	if begin.Secret == "" || !strings.HasPrefix(begin.URL, "otpauth://") {
		t.Fatalf("%+v", begin)
	}
	h.mustCall("POST", "/api/totp/confirm", map[string]string{"code": "000000"}, nil, 400)
	h.mustCall("POST", "/api/totp/confirm", map[string]string{"code": auth.TOTPCode(begin.Secret, h.now())}, nil, 200)

	h.mustCall("DELETE", "/api/session", nil, nil, 200)
	var step map[string]any
	h.mustCall("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": goodPassword}, &step, 200)
	if step["need_code"] != true {
		t.Fatalf("a code should be requested: %v", step)
	}
	h.mustCall("GET", "/api/home", nil, nil, 401)
	// The code used to turn two-step on is spent; the next one works.
	h.mustCall("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": goodPassword, "code": auth.TOTPCode(begin.Secret, h.now())}, nil, 401)
	h.advance(30 * time.Second)
	h.mustCall("POST", "/api/session", map[string]string{"email": "maria@example.com", "password": goodPassword, "code": auth.TOTPCode(begin.Secret, h.now())}, nil, 200)
	h.mustCall("GET", "/api/home", nil, nil, 200)
}

func TestServerAddressNameVersusIP(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var out map[string]string
	h.mustCall("PUT", "/api/network/server-address", map[string]string{"address": "vpn.example.com"}, &out, 200)
	if out["server_address_kind"] != "name" {
		t.Fatalf("%v", out)
	}
	h.mustCall("PUT", "/api/network/server-address", map[string]string{"address": "203.0.113.10"}, &out, 200)
	if out["server_address_kind"] != "ip" {
		t.Fatalf("%v", out)
	}
	for _, bad := range []string{"", "https://vpn.example.com", "vpn.example.com:51820", "has space.example", "127.0.0.1", "bad_name.example"} {
		var e map[string]string
		h.mustCall("PUT", "/api/network/server-address", map[string]string{"address": bad}, &e, 400)
		if e["error"] == "" {
			t.Errorf("%q: no message", bad)
		}
	}
}

// ---- servers ----

func TestEnrollTokenWorksOnceAndExpires(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var created struct {
		Install map[string]string `json:"install"`
	}
	h.mustCall("POST", "/api/servers", nil, &created, 201)
	tok := tokenRe.FindStringSubmatch(created.Install["docker"])[1]
	_, pub, _ := wgKeypair(t)

	post := func(token string) int {
		res := h.call("POST", "/gateway/v1/enroll", map[string]string{"token": token, "public_key": pub, "name": "x"}, nil)
		return res.StatusCode
	}
	if post("wrong") != 401 {
		t.Fatal("wrong token accepted")
	}
	if post(tok) != 200 {
		t.Fatal("good token rejected")
	}
	if post(tok) != 401 {
		t.Fatal("token reused")
	}

	h2 := newHarness(t)
	h2.setup()
	h2.mustCall("POST", "/api/servers", nil, &created, 201)
	tok2 := tokenRe.FindStringSubmatch(created.Install["docker"])[1]
	h2.advance(25 * time.Hour)
	if res := h2.call("POST", "/gateway/v1/enroll", map[string]string{"token": tok2, "public_key": pub}, nil); res.StatusCode != 401 {
		t.Fatalf("expired token accepted: %d", res.StatusCode)
	}
}

func TestOnlyOneServerAtATimeInPhaseOne(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	h.mustCall("POST", "/api/servers", nil, nil, 409)
}

func TestGatewayEndpointsRejectUnknownServers(t *testing.T) {
	h := newHarness(t)
	h.setup()
	for _, tc := range []struct{ method, path string }{{"GET", "/gateway/v1/desired?since=-1"}, {"POST", "/gateway/v1/report"}} {
		req, _ := http.NewRequest(tc.method, h.ts.URL+tc.path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer nonsense")
		res, _ := http.DefaultClient.Do(req)
		if res.StatusCode != 401 {
			t.Errorf("%s %s: %d", tc.method, tc.path, res.StatusCode)
		}
		req2, _ := http.NewRequest(tc.method, h.ts.URL+tc.path, strings.NewReader("{}"))
		res2, _ := http.DefaultClient.Do(req2)
		if res2.StatusCode != 401 {
			t.Errorf("%s %s without token: %d", tc.method, tc.path, res2.StatusCode)
		}
	}
}

func TestServerStatusFollowsCheckIns(t *testing.T) {
	h := newHarness(t)
	g, _ := h.connectedServer(t.TempDir())
	var servers []map[string]any
	h.mustCall("GET", "/api/servers", nil, &servers, 200)
	if servers[0]["status"] != "online" {
		t.Fatalf("%v", servers)
	}
	h.advance(2 * time.Minute)
	h.mustCall("GET", "/api/servers", nil, &servers, 200)
	if servers[0]["status"] != "offline" {
		t.Fatalf("%v", servers)
	}
	var home map[string]any
	h.mustCall("GET", "/api/home", nil, &home, 200)
	att := home["attention"].([]any)
	if len(att) != 1 || att[0].(map[string]any)["kind"] != "server_offline" {
		t.Fatalf("attention: %v", att)
	}
	g.ReportNow(context.Background())
	h.mustCall("GET", "/api/servers", nil, &servers, 200)
	if servers[0]["status"] != "online" {
		t.Fatalf("%v", servers)
	}
}

// ---- adding devices ----

func TestAddingADeviceNeedsAServerFirst(t *testing.T) {
	h := newHarness(t)
	h.setup()
	p := h.person("Sam")
	var e map[string]string
	h.mustCall("POST", "/api/devices", map[string]string{"person_id": p, "platform": "ios"}, &e, 409)
	if e["error"] == "" {
		t.Fatal("needs a plain message")
	}
}

func TestAddDeviceReturnsAOneTimeCodeAndShowsAsWaiting(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	a := h.addDevice(sam, "ios")
	if a.Device["name"] != "Sam's iPhone" || a.Device["status"] != "waiting" {
		t.Fatalf("%v", a.Device)
	}
	for _, want := range []string{"[Interface]", "PrivateKey = ", "Endpoint = vpn.example.com:51820", "PresharedKey = ", "Address = 10.", "/32"} {
		if !strings.Contains(a.Config, want) {
			t.Errorf("config missing %q:\n%s", want, a.Config)
		}
	}
	if strings.Contains(a.Config, "PersistentKeepalive") {
		t.Error("phones must not keep the connection alive")
	}
	if a.QR == "" || a.Filename != "sams-iphone.conf" {
		t.Fatalf("qr=%d file=%q", len(a.QR), a.Filename)
	}

	// Laptops keep the connection alive; a second device for the same person gets a distinct name.
	b := h.addDevice(sam, "ios")
	if b.Device["name"] != "Sam's iPhone 2" {
		t.Fatalf("%v", b.Device["name"])
	}
	c := h.addDevice(sam, "windows")
	if !strings.Contains(c.Config, "PersistentKeepalive = 25") {
		t.Error("computers should keep the connection alive")
	}

	// The code is shown once: nothing in the API returns a private key again.
	var detail map[string]any
	h.mustCall("GET", "/api/devices/"+a.Device["id"].(string), nil, &detail, 200)
	if _, ok := detail["config"]; ok {
		t.Fatal("config must not be retrievable")
	}
	adv := detail["advanced"].(map[string]any)
	if adv["public_key"] == "" || adv["address"] == "" {
		t.Fatalf("%v", adv)
	}
}

func TestAddressesAreUniqueAndFollowTheNetworkRange(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		a := h.addDevice(sam, "android")
		line := ""
		for _, l := range strings.Split(a.Config, "\n") {
			if strings.HasPrefix(l, "Address = ") {
				line = l
			}
		}
		if line == "" || seen[line] {
			t.Fatalf("address %q duplicated or missing", line)
		}
		seen[line] = true
	}
}

func TestBadDeviceRequests(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	h.mustCall("POST", "/api/devices", map[string]string{"person_id": sam, "platform": "toaster"}, nil, 400)
	h.mustCall("POST", "/api/devices", map[string]string{"person_id": "nope", "platform": "ios"}, nil, 400)
}

// ---- the whole loop with a real server role ----

func TestServerReceivesDevicesAndReportsConnections(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	ctx := context.Background()
	sam := h.person("Sam")
	a := h.addDevice(sam, "ios")
	id := a.Device["id"].(string)

	d, err := g.Sync(ctx)
	if err != nil || len(d.Add) != 1 {
		t.Fatalf("sync: %+v %v", d, err)
	}
	if d2, _ := g.Sync(ctx); !d2.IsEmpty() {
		t.Fatalf("second sync must be empty: %+v", d2)
	}
	peers, _ := fake.Peers(ctx, "wardline0")
	if len(peers) != 1 || peers[0].PresharedKey == "" || peers[0].AllowedIPs[0].Bits() != 32 {
		t.Fatalf("%+v", peers)
	}

	// The phone connects.
	fake.SimulateHandshake("wardline0", peers[0].PublicKey, 100, 200, "198.51.100.4:5555")
	if err := g.ReportNow(ctx); err != nil {
		t.Fatal(err)
	}
	var dev map[string]any
	h.mustCall("GET", "/api/devices/"+id, nil, &dev, 200)
	if dev["status"] != "connected" || dev["last_seen_at"] == nil {
		t.Fatalf("%v", dev)
	}

	// Later it goes quiet: not an alarm, just "not seen recently".
	h.advance(10 * time.Minute)
	h.mustCall("GET", "/api/devices/"+id, nil, &dev, 200)
	if dev["status"] != "not_seen" {
		t.Fatalf("%v", dev["status"])
	}
	var home map[string]any
	h.mustCall("GET", "/api/home", nil, &home, 200)
	for _, a := range home["attention"].([]any) {
		if a.(map[string]any)["kind"] == "device_unseen_30_days" {
			t.Fatal("a sleeping phone is not an alarm")
		}
	}
	// After 31 days it earns a gentle prompt.
	h.advance(31 * 24 * time.Hour)
	h.login()
	h.mustCall("GET", "/api/home", nil, &home, 200)
	found := false
	for _, a := range home["attention"].([]any) {
		if a.(map[string]any)["kind"] == "device_unseen_30_days" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 30-day prompt: %v", home["attention"])
	}

	var act []map[string]any
	h.mustCall("GET", "/api/activity", nil, &act, 200)
	texts := ""
	for _, e := range act {
		texts += e["text"].(string) + "\n"
	}
	for _, want := range []string{"added Sam's iPhone.", "Sam's iPhone connected for the first time."} {
		if !strings.Contains(texts, want) {
			t.Errorf("activity missing %q:\n%s", want, texts)
		}
	}
}

func TestRemovingAccessReachesTheServerWithinSeconds(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.Run(ctx)

	sam := h.person("Sam")
	a := h.addDevice(sam, "android")
	id := a.Device["id"].(string)
	waitUntil(t, "peer to appear", func() bool { p, _ := fake.Peers(ctx, "wardline0"); return len(p) == 1 })

	start := time.Now()
	h.mustCall("DELETE", "/api/devices/"+id, nil, nil, 200)
	waitUntil(t, "peer to disappear", func() bool { p, _ := fake.Peers(ctx, "wardline0"); return len(p) == 0 })
	if time.Since(start) > 3*time.Second {
		t.Fatalf("removal took %v", time.Since(start))
	}

	h.mustCall("GET", "/api/devices/"+id, nil, nil, 404)
	h.mustCall("DELETE", "/api/devices/"+id, nil, nil, 404)

	// The address is held back for 7 days (quarantine).
	var state string
	h.st.DB().QueryRow(`SELECT state FROM ip_allocations WHERE device_id=?`, id).Scan(&state)
	if state != "quarantined" {
		t.Fatalf("address state = %q", state)
	}
	var keyState string
	h.st.DB().QueryRow(`SELECT state FROM device_keys WHERE device_id=?`, id).Scan(&keyState)
	if keyState != "retired" {
		t.Fatalf("key state = %q", keyState)
	}
	var n int
	h.st.DB().QueryRow(`SELECT count(*) FROM preshared_keys WHERE device_id=?`, id).Scan(&n)
	if n != 0 {
		t.Fatal("preshared key should be deleted")
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHomeGroupsDevicesByPerson(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam, alex := h.person("Sam"), h.person("Alex")
	h.addDevice(sam, "ios")
	h.addDevice(sam, "windows")
	_ = alex
	var home struct {
		People []struct {
			Name    string
			Devices []map[string]any
		}
		Server map[string]any
	}
	h.mustCall("GET", "/api/home", nil, &home, 200)
	if len(home.People) != 2 || home.People[0].Name != "Alex" || len(home.People[0].Devices) != 0 || len(home.People[1].Devices) != 2 {
		t.Fatalf("%+v", home.People)
	}
	if home.Server["status"] != "online" {
		t.Fatalf("%v", home.Server)
	}
}

func TestRefreshPromptAppearsAfter180DaysForPhones(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	h.addDevice(h.person("Sam"), "ios")
	h.advance(181 * 24 * time.Hour)
	h.login()
	var home map[string]any
	h.mustCall("GET", "/api/home", nil, &home, 200)
	found := false
	for _, a := range home["attention"].([]any) {
		if a.(map[string]any)["kind"] == "refresh_due" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%v", home["attention"])
	}
}

// ---- Activity ----

func TestActivityFiltersExportAndVerify(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam, alex := h.person("Sam"), h.person("Alex")
	h.addDevice(sam, "ios")
	h.addDevice(alex, "android")

	var all, bySam []map[string]any
	h.mustCall("GET", "/api/activity", nil, &all, 200)
	h.mustCall("GET", "/api/activity?person_id="+sam, nil, &bySam, 200)
	if len(bySam) == 0 || len(bySam) >= len(all) {
		t.Fatalf("filter: %d of %d", len(bySam), len(all))
	}
	for _, e := range bySam {
		if strings.Contains(e["text"].(string), "Alex's") {
			t.Fatalf("wrong person in filter: %v", e)
		}
	}

	res := h.call("GET", "/api/activity.csv", nil, nil)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if n, err := audit.New(h.st.DB()).Verify(context.Background()); err != nil || n < 5 {
		t.Fatalf("verify: %d %v", n, err)
	}
}

func TestErrorResponsesNeverShowInternals(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	// Break the database underneath the API and check the message people would see.
	h.st.DB().Exec(`DROP TABLE preshared_keys`)
	sam := h.person("Sam")
	var e map[string]string
	h.mustCall("POST", "/api/devices", map[string]string{"person_id": sam, "platform": "ios"}, &e, 500)
	low := strings.ToLower(e["error"])
	for _, bad := range []string{"sql", "table", "no such", "panic", "goroutine", ".go:"} {
		if strings.Contains(low, bad) {
			t.Fatalf("internal detail leaked: %q", e["error"])
		}
	}
}

func TestStaticFallbackServesTheWebApp(t *testing.T) {
	h := newHarness(t)
	h.api.d.Static = nil
	rec := httptest.NewRecorder()
	h.api.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 404 {
		t.Fatalf("no static handler configured should 404, got %d", rec.Code)
	}
}

func TestInstallInstructionsIncludeALinuxServiceCommand(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var created struct {
		Install map[string]string `json:"install"`
	}
	h.mustCall("POST", "/api/servers", nil, &created, 201)
	cmd := created.Install["linux"]
	docker := tokenRe.FindStringSubmatch(created.Install["docker"])[1]
	for _, want := range []string{"install-linux.sh", "gateway", docker} {
		if !strings.Contains(cmd, want) {
			t.Errorf("linux command %q is missing %q", cmd, want)
		}
	}
	if strings.Contains(cmd, "\n") {
		t.Errorf("the linux command should be one line: %q", cmd)
	}
}
