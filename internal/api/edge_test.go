package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The read-only API for the Edge. The shape is the contract in the Edge project's specs/WARDLINE-CONTRACT.md.

var hostnameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// checkMembersShape validates a members response against the contract and returns what it found.
func checkMembersShape(t *testing.T, raw []byte) (map[string]any, []map[string]any) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	for _, k := range []string{"version", "group_id", "generated_at", "cursor", "members"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("response lacks %q: %s", k, raw)
		}
	}
	if doc["version"] != "v1" {
		t.Fatalf("version %v", doc["version"])
	}
	for _, k := range []string{"group_id", "cursor"} {
		if s, ok := doc[k].(string); !ok || s == "" {
			t.Fatalf("%s must be a non-empty string: %v", k, doc[k])
		}
	}
	if _, err := time.Parse(time.RFC3339, doc["generated_at"].(string)); err != nil {
		t.Fatalf("generated_at: %v", err)
	}
	list, ok := doc["members"].([]any)
	if !ok {
		t.Fatalf("members must be a list (even an empty one): %v", doc["members"])
	}
	seenLabels := map[string]bool{}
	var out []map[string]any
	for _, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			t.Fatalf("a member is not an object: %v", x)
		}
		out = append(out, m)
		for _, k := range []string{"id", "name", "hostname_label", "overlay_address", "status"} {
			if s, ok := m[k].(string); !ok || s == "" {
				t.Fatalf("member %v: %s must be a non-empty string", m["id"], k)
			}
		}
		if !hostnameRE.MatchString(m["hostname_label"].(string)) {
			t.Fatalf("hostname_label %q is not 1 to 63 lowercase letters, digits and hyphens", m["hostname_label"])
		}
		if seenLabels[m["hostname_label"].(string)] {
			t.Fatalf("hostname_label %q is not unique in the group", m["hostname_label"])
		}
		seenLabels[m["hostname_label"].(string)] = true
		if _, err := netip.ParseAddr(m["overlay_address"].(string)); err != nil {
			t.Fatalf("overlay_address %q is not an IP address", m["overlay_address"])
		}
		switch m["status"] {
		case "connected", "not_seen_recently", "waiting", "removed":
		default:
			t.Fatalf("status %v is not one of the four the contract allows", m["status"])
		}
		roles, ok := m["roles"].([]any)
		if !ok {
			t.Fatalf("roles must be a list: %v", m["roles"])
		}
		for _, r := range roles {
			if _, ok := r.(string); !ok {
				t.Fatalf("a role is not text: %v", r)
			}
		}
		if ls, ok := m["last_seen_at"]; ok && ls != nil {
			if _, err := time.Parse(time.RFC3339, ls.(string)); err != nil {
				t.Fatalf("last_seen_at: %v", err)
			}
		}
		labels, ok := m["labels"].(map[string]any)
		if !ok {
			t.Fatalf("labels must be an object: %v", m["labels"])
		}
		for k, v := range labels {
			if _, ok := v.(string); !ok {
				t.Fatalf("label %s is not text", k)
			}
		}
	}
	return doc, out
}

func TestTheContractSampleItselfPassesTheShapeCheck(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/contract/members.json")
	if err != nil {
		t.Fatal(err)
	}
	checkMembersShape(t, raw)
}

// ---- helpers ----

func (h *harness) makeToken(name string) (id, token string) {
	h.t.Helper()
	var out struct {
		ID    string `json:"id"`
		Token string `json:"token"`
		Scope string `json:"scope"`
	}
	h.mustCall("POST", "/api/tokens", map[string]string{"name": name}, &out, 201)
	if out.Token == "" || out.Scope != "edge:read" {
		h.t.Fatalf("%+v", out)
	}
	return out.ID, out.Token
}

func (h *harness) edgeGet(path, token string) (*http.Response, []byte) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.ts.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req) // no cookies: a bearer token is all the Edge has
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func (h *harness) members(token string) (map[string]any, []map[string]any) {
	h.t.Helper()
	res, b := h.edgeGet("/api/v1/edge/members", token)
	if res.StatusCode != 200 {
		h.t.Fatalf("members: %d %s", res.StatusCode, b)
	}
	return checkMembersShape(h.t, b)
}

func byLabel(ms []map[string]any, label string) map[string]any {
	for _, m := range ms {
		if m["hostname_label"] == label {
			return m
		}
	}
	return nil
}

// ---- tokens ----

func TestAnOperatorMakesAReadOnlyKeyThatIsShownOnceAndStoredOnlyAsAHash(t *testing.T) {
	h := newHarness(t)
	h.setup()
	id, token := h.makeToken("Front door Edge")
	if !strings.HasPrefix(token, "wdl_edge_") || len(token) < 40 {
		t.Fatalf("token %q", token)
	}
	var list []map[string]any
	h.mustCall("GET", "/api/tokens", nil, &list, 200)
	if len(list) != 1 || list[0]["id"] != id || list[0]["name"] != "Front door Edge" || list[0]["scope"] != "edge:read" {
		t.Fatalf("%v", list)
	}
	raw, _ := json.Marshal(list)
	if bytes.Contains(raw, []byte(token)) || list[0]["token"] != nil {
		t.Fatal("the key must never be shown again")
	}
	h.st.DB().Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
	dump, _ := os.ReadFile(h.dbPath)
	wal, _ := os.ReadFile(h.dbPath + "-wal")
	if bytes.Contains(dump, []byte(token)) || bytes.Contains(wal, []byte(token)) {
		t.Fatal("the key is stored in the database; only its hash may be")
	}
	var text string
	h.st.DB().QueryRow(`SELECT plain_text FROM audit_events WHERE action='token.created'`).Scan(&text)
	if !strings.Contains(text, "Front door Edge") || strings.Contains(text, token) {
		t.Fatalf("activity says %q", text)
	}
	h.mustCall("POST", "/api/tokens", map[string]string{"name": ""}, nil, 400)
	h.hc.Jar = nil
	h.mustCall("POST", "/api/tokens", map[string]string{"name": "x"}, nil, 401)
	h.mustCall("GET", "/api/tokens", nil, nil, 401)
}

func TestARevokedKeyStopsWorkingAtOnce(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	id, token := h.makeToken("Edge")
	if res, _ := h.edgeGet("/api/v1/edge/members", token); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	h.mustCall("DELETE", "/api/tokens/"+id, nil, nil, 200)
	if res, _ := h.edgeGet("/api/v1/edge/members", token); res.StatusCode != 401 {
		t.Fatalf("a revoked key got %d", res.StatusCode)
	}
	h.mustCall("DELETE", "/api/tokens/nonexistent", nil, nil, 404)
}

func TestTheEdgeEndpointsRefuseAnythingButAGoodKey(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	_, token := h.makeToken("Edge")
	for _, path := range []string{"/api/v1/edge/members", "/api/v1/edge/events"} {
		if res, _ := h.edgeGet(path, ""); res.StatusCode != 401 {
			t.Errorf("%s with no key: %d", path, res.StatusCode)
		}
		if res, _ := h.edgeGet(path, "wdl_edge_not-a-real-key"); res.StatusCode != 401 {
			t.Errorf("%s with a wrong key: %d", path, res.StatusCode)
		}
		// A signed-in administrator's cookie is not a key.
		req, _ := http.NewRequest("GET", h.ts.URL+path, nil)
		res, _ := h.hc.Do(req)
		res.Body.Close()
		if res.StatusCode != 401 {
			t.Errorf("%s with only a sign-in cookie: %d", path, res.StatusCode)
		}
	}
	// A key with some other scope may not read.
	h.st.DB().Exec(`INSERT INTO api_tokens(id,name,scope,token_hash,created_at) VALUES('x','other','something:else',?,?)`, tokenHash("wdl_edge_other"), time.Now().Format(time.RFC3339))
	if res, _ := h.edgeGet("/api/v1/edge/members", "wdl_edge_other"); res.StatusCode != 403 {
		t.Errorf("a key without the scope: %d", res.StatusCode)
	}
	// And the key opens nothing else.
	for _, path := range []string{"/api/devices", "/api/home", "/api/tokens", "/api/network", "/api/activity"} {
		if res, _ := h.edgeGet(path, token); res.StatusCode != 401 {
			t.Errorf("the Edge key reached %s (%d)", path, res.StatusCode)
		}
	}
}

// ---- members ----

func TestTheMemberListHasMachinesAndTheServerInTheContractsShape(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	office := h.addNamed("Office PC")
	studio := h.addNamed("Studio Mac")
	h.mustCall("DELETE", "/api/devices/"+studio.ID, nil, nil, 200)
	_, token := h.makeToken("Edge")

	doc, ms := h.members(token)
	if len(ms) != 2 {
		t.Fatalf("want the machine and the server, got %d: %v", len(ms), ms)
	}
	var gid string
	h.st.DB().QueryRow(`SELECT id FROM groups`).Scan(&gid)
	if doc["group_id"] != gid {
		t.Fatalf("group_id %v, want %s", doc["group_id"], gid)
	}
	m := byLabel(ms, "office-pc")
	if m == nil || m["id"] != office.ID || m["name"] != "Office PC" || m["status"] != "waiting" {
		t.Fatalf("%v", m)
	}
	var ip string
	h.st.DB().QueryRow(`SELECT tunnel_ip FROM devices WHERE id=?`, office.ID).Scan(&ip)
	if m["overlay_address"] != ip {
		t.Fatalf("address %v want %s", m["overlay_address"], ip)
	}
	srv := byLabel(ms, "server")
	if srv == nil || srv["id"] != h.serverID() {
		t.Fatalf("the server must be listed as 'server': %v", ms)
	}
	if roles := srv["roles"].([]any); len(roles) != 1 || roles[0] != "gateway" {
		t.Fatalf("a server has the gateway role: %v", srv["roles"])
	}
	if byLabel(ms, "studio-mac") != nil {
		t.Fatal("a removed machine is not in the list")
	}
}

func TestStatusFollowsWhenTheMachineWasLastSeen(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	_, token := h.makeToken("Edge")
	setSeen := func(ago time.Duration) {
		h.st.DB().Exec(`UPDATE devices SET last_seen_at=?, status='active' WHERE id=?`, h.now().Add(-ago).UTC().Format(time.RFC3339), a.ID)
	}
	setSeen(10 * time.Second)
	_, ms := h.members(token)
	if m := byLabel(ms, "office-pc"); m["status"] != "connected" || m["last_seen_at"] == nil {
		t.Fatalf("%v", m)
	}
	setSeen(2 * time.Hour)
	_, ms = h.members(token)
	if m := byLabel(ms, "office-pc"); m["status"] != "not_seen_recently" {
		t.Fatalf("%v", m)
	}
}

func TestLabelsAreStoredAndReturnedAndNeverReadByWardline(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	_, token := h.makeToken("Edge")
	h.mustCall("PUT", "/api/devices/"+a.ID+"/labels", map[string]any{"labels": map[string]string{"edge.upstream_port": "8080", "edge.scheme": "http", "anything.else": "kept"}}, nil, 200)
	_, ms := h.members(token)
	labels := byLabel(ms, "office-pc")["labels"].(map[string]any)
	if labels["edge.upstream_port"] != "8080" || labels["edge.scheme"] != "http" || labels["anything.else"] != "kept" {
		t.Fatalf("%v", labels)
	}
	// Replacing the labels replaces them all.
	h.mustCall("PUT", "/api/devices/"+a.ID+"/labels", map[string]any{"labels": map[string]string{"edge.enabled": "false"}}, nil, 200)
	_, ms = h.members(token)
	labels = byLabel(ms, "office-pc")["labels"].(map[string]any)
	if len(labels) != 1 || labels["edge.enabled"] != "false" {
		t.Fatalf("%v", labels)
	}
	// Bad labels are refused in plain words.
	for name, in := range map[string]map[string]string{
		"a bad key":    {"Has Spaces": "x"},
		"an empty key": {"": "x"},
		"a long value": {"k": strings.Repeat("x", 201)},
	} {
		var e map[string]string
		h.mustCall("PUT", "/api/devices/"+a.ID+"/labels", map[string]any{"labels": in}, &e, 400)
		if e["error"] == "" {
			t.Errorf("%s: no message", name)
		}
	}
	many := map[string]string{}
	for i := 0; i < 21; i++ {
		many[fmt.Sprintf("k%d", i)] = "v"
	}
	h.mustCall("PUT", "/api/devices/"+a.ID+"/labels", map[string]any{"labels": many}, nil, 400)
	h.mustCall("PUT", "/api/devices/nonexistent/labels", map[string]any{"labels": map[string]string{}}, nil, 404)
	h.hc.Jar = nil
	h.mustCall("PUT", "/api/devices/"+a.ID+"/labels", map[string]any{"labels": map[string]string{}}, nil, 401)
}

func TestNoPrivateKeyIsInWhatTheEdgeIsSent(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addDevice(h.person("Sam"), "linux")
	priv := cfgPrivateRe.FindStringSubmatch(a.Config)[1]
	_, token := h.makeToken("Edge")
	res, b := h.edgeGet("/api/v1/edge/members", token)
	if res.StatusCode != 200 || bytes.Contains(b, []byte(priv)) || bytes.Contains(b, []byte(token)) {
		t.Fatalf("members response leaks a secret or failed: %d", res.StatusCode)
	}
	psk := cfgPSKRe.FindStringSubmatch(a.Config)[1]
	if bytes.Contains(b, []byte(psk)) {
		t.Fatal("members response contains a preshared key")
	}
	for _, key := range []string{"public_key", "private", "preshared", "token"} {
		if strings.Contains(string(b), `"`+key) {
			t.Errorf("members response has a %q field", key)
		}
	}
}

// ---- events ----

type sseEvent struct{ id, kind, data string }

// openEvents opens the event stream and delivers events and heartbeats on channels until the test ends.
func (h *harness) openEvents(ctx context.Context, token, lastID string) (status int, events chan sseEvent, pings chan struct{}) {
	h.t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", h.ts.URL+"/api/v1/edge/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	events, pings = make(chan sseEvent, 64), make(chan struct{}, 64)
	if res.StatusCode != 200 {
		res.Body.Close()
		return res.StatusCode, events, pings
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		h.t.Fatalf("content type %q", ct)
	}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		var cur sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur.kind != "" {
					events <- cur
				}
				cur = sseEvent{}
			case strings.HasPrefix(line, ": ping"):
				pings <- struct{}{}
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.kind = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return 200, events, pings
}

func nextEvent(t *testing.T, ch chan sseEvent, kind string) sseEvent {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e := <-ch:
			if kind == "" || e.kind == kind {
				return e
			}
		case <-deadline:
			t.Fatalf("no %q event arrived", kind)
		}
	}
}

func TestChangesArriveAsEventsWithTheMemberRecord(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	h.api.edgePoll, h.api.edgeHeartbeat = 40*time.Millisecond, 100*time.Millisecond
	_, token := h.makeToken("Edge")
	h.members(token) // the Edge starts with a full list, which also fixes the starting point for changes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status, events, pings := h.openEvents(ctx, token, "")
	if status != 200 {
		t.Fatalf("status %d", status)
	}

	a := h.addNamed("Office PC")
	e := nextEvent(t, events, "member_added")
	var rec map[string]any
	if err := json.Unmarshal([]byte(e.data), &rec); err != nil {
		t.Fatalf("data is not JSON: %v", err)
	}
	if rec["id"] != a.ID || rec["hostname_label"] != "office-pc" || e.id == "" {
		t.Fatalf("%v (id %q)", rec, e.id)
	}
	first, _ := strconv.Atoi(e.id)

	h.mustCall("PATCH", "/api/devices/"+a.ID, map[string]string{"name": "Back PC"}, nil, 200)
	u := nextEvent(t, events, "member_updated")
	json.Unmarshal([]byte(u.data), &rec)
	if rec["name"] != "Back PC" || rec["hostname_label"] != "back-pc" {
		t.Fatalf("%v", rec)
	}
	if n, _ := strconv.Atoi(u.id); n <= first {
		t.Fatalf("ids must go up: %d then %d", first, n)
	}

	h.st.DB().Exec(`UPDATE devices SET last_seen_at=?, status='active' WHERE id=?`, h.now().UTC().Format(time.RFC3339), a.ID)
	s := nextEvent(t, events, "member_status_changed")
	json.Unmarshal([]byte(s.data), &rec)
	if rec["status"] != "connected" {
		t.Fatalf("%v", rec)
	}

	h.mustCall("PUT", "/api/devices/"+a.ID+"/labels", map[string]any{"labels": map[string]string{"edge.scheme": "https"}}, nil, 200)
	l := nextEvent(t, events, "member_updated")
	json.Unmarshal([]byte(l.data), &rec)
	if rec["labels"].(map[string]any)["edge.scheme"] != "https" {
		t.Fatalf("%v", rec)
	}

	h.mustCall("DELETE", "/api/devices/"+a.ID, nil, nil, 200)
	r := nextEvent(t, events, "member_removed")
	json.Unmarshal([]byte(r.data), &rec)
	if rec["id"] != a.ID || rec["status"] != "removed" {
		t.Fatalf("%v", rec)
	}

	select {
	case <-pings:
	case <-time.After(5 * time.Second):
		t.Fatal("no heartbeat comment arrived")
	}
}

func TestAnEdgeThatReconnectsGetsWhatItMissed(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	h.api.edgePoll = 40 * time.Millisecond
	_, token := h.makeToken("Edge")
	doc, _ := h.members(token)
	cursor := doc["cursor"].(string)

	// Things happen while nobody is listening.
	h.addNamed("One")
	h.addNamed("Two")
	h.members(token) // a full resync, as the Edge does, finds the changes and moves the cursor on
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status, events, _ := h.openEvents(ctx, token, cursor)
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	a := nextEvent(t, events, "member_added")
	b := nextEvent(t, events, "member_added")
	var ra, rb map[string]any
	json.Unmarshal([]byte(a.data), &ra)
	json.Unmarshal([]byte(b.data), &rb)
	if ra["hostname_label"] != "one" || rb["hostname_label"] != "two" {
		t.Fatalf("%v %v", ra["hostname_label"], rb["hostname_label"])
	}
}

func TestACursorThatIsTooOldOrNotOursGetsGone(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	_, token := h.makeToken("Edge")
	h.members(token)
	for _, id := range []string{"999999", "not-a-number", "-4"} {
		res, _ := h.edgeGetEvents(token, id)
		if res != 410 {
			t.Errorf("Last-Event-ID %q got %d, want 410 so the Edge does a full resync", id, res)
		}
	}
	// Events older than the ones still kept are gone too.
	h.st.DB().Exec(`INSERT INTO edge_events(seq,kind,member_id,data,created_at) VALUES(50,'member_added','x','{}','2026-01-01T00:00:00Z'),(51,'member_added','y','{}','2026-01-01T00:00:00Z')`)
	if res, _ := h.edgeGetEvents(token, "10"); res != 410 {
		t.Errorf("an id from before the oldest kept event got %d, want 410", res)
	}
}

func (h *harness) edgeGetEvents(token, lastID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", h.ts.URL+"/api/v1/edge/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-ID", lastID)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

func TestOnlyTheNewestEventsAreKept(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	_, token := h.makeToken("Edge")
	h.members(token)
	ctx := context.Background()
	tx, _ := h.st.DB().BeginTx(ctx, nil)
	for i := 0; i < 1100; i++ {
		tx.Exec(`INSERT INTO edge_events(kind,member_id,data,created_at) VALUES('member_updated','x','{}','2026-01-01T00:00:00Z')`)
	}
	tx.Commit()
	h.addNamed("Trigger")
	h.members(token)
	var n int
	h.st.DB().QueryRow(`SELECT count(*) FROM edge_events`).Scan(&n)
	if n > 1000 {
		t.Fatalf("%d events kept, want at most 1000", n)
	}
}
