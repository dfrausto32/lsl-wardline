package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/agent"
	"lsl-wardline/internal/gateway"
	"lsl-wardline/internal/wg"
)

func (h *harness) agentState(e enrolled) agent.DesiredState {
	h.t.Helper()
	var s gateway.Signed
	h.mustAgent("GET", "/agent/v1/desired?since=-1", e.resp.AuthToken, nil, &s, 200)
	st, err := agent.Verify(e.resp.ControlKey, s)
	if err != nil {
		h.t.Fatal(err)
	}
	return st
}

func serverPeerKeys(t *testing.T, fake *wg.Fake) []string {
	t.Helper()
	peers, _ := fake.Peers(context.Background(), "wardline0")
	var keys []string
	for _, p := range peers {
		keys = append(keys, p.PublicKey)
	}
	return keys
}

func TestRefreshSwapsTheKeyAndTheServerFollows(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	ctx := context.Background()
	e := h.enrollAgent(h.addAgentDevice(h.person("Sam"), "linux").Token)
	g.Sync(ctx)
	if k := serverPeerKeys(t, fake); len(k) != 1 || k[0] != e.pub {
		t.Fatalf("%v", k)
	}

	st := h.agentState(e)
	if st.PublicKey != e.pub || st.RefreshDue == nil || st.RefreshDue.Before(h.now().Add(89*24*time.Hour)) {
		t.Fatalf("instructions: key %q, due %v", st.PublicKey, st.RefreshDue)
	}

	_, newPub, _ := wg.GenerateKeypair()
	h.mustAgent("POST", "/agent/v1/refresh", e.resp.AuthToken, agent.RefreshRequest{PublicKey: newPub}, nil, 200)
	g.Sync(ctx)
	if k := serverPeerKeys(t, fake); len(k) != 1 || k[0] != newPub {
		t.Fatalf("the server should now accept only the new key: %v", k)
	}
	st = h.agentState(e)
	if st.PublicKey != newPub || st.RefreshDue.Before(h.now().Add(89*24*time.Hour)) {
		t.Fatalf("%q %v", st.PublicKey, st.RefreshDue)
	}
	var states []string
	rows, _ := h.st.DB().Query(`SELECT state FROM device_keys WHERE device_id=? ORDER BY created_at, rowid`, e.resp.DeviceID)
	for rows.Next() {
		var s string
		rows.Scan(&s)
		states = append(states, s)
	}
	rows.Close()
	if strings.Join(states, ",") != "retired,active" {
		t.Fatalf("key states %v", states)
	}
	var recs []map[string]any
	h.mustCall("GET", "/api/activity", nil, &recs, 200)
	var said bool
	for _, r := range recs {
		if strings.Contains(r["text"].(string), "refreshed its security") && !strings.Contains(r["text"].(string), newPub) {
			said = true
		}
	}
	if !said {
		t.Fatalf("no plain sentence about the refresh: %v", recs)
	}
}

func TestRefreshRequestsAreChecked(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	e := h.enrollAgent(h.addAgentDevice(sam, "linux").Token)
	other := h.enrollAgent(h.addAgentDevice(sam, "windows").Token)
	_, fresh, _ := wg.GenerateKeypair()

	h.mustAgent("POST", "/agent/v1/refresh", "", agent.RefreshRequest{PublicKey: fresh}, nil, 401)
	h.mustAgent("POST", "/agent/v1/refresh", e.resp.AuthToken, agent.RefreshRequest{PublicKey: "nope"}, nil, 400)
	h.mustAgent("POST", "/agent/v1/refresh", e.resp.AuthToken, agent.RefreshRequest{PublicKey: e.pub}, nil, 400)
	h.mustAgent("POST", "/agent/v1/refresh", e.resp.AuthToken, agent.RefreshRequest{PublicKey: other.pub}, nil, 400)
	if st := h.agentState(e); st.PublicKey != e.pub {
		t.Fatal("a refused refresh changed the key")
	}
}

func TestRollbackGoesBackToTheOldKeyOnlyWithinAnHour(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	ctx := context.Background()
	e := h.enrollAgent(h.addAgentDevice(h.person("Sam"), "linux").Token)

	h.mustAgent("POST", "/agent/v1/refresh/rollback", e.resp.AuthToken, struct{}{}, nil, 409) // nothing to go back to

	_, newPub, _ := wg.GenerateKeypair()
	h.mustAgent("POST", "/agent/v1/refresh", e.resp.AuthToken, agent.RefreshRequest{PublicKey: newPub}, nil, 200)
	h.mustAgent("POST", "/agent/v1/refresh/rollback", e.resp.AuthToken, struct{}{}, nil, 200)
	g.Sync(ctx)
	if k := serverPeerKeys(t, fake); len(k) != 1 || k[0] != e.pub {
		t.Fatalf("the server should be back on the old key: %v", k)
	}
	if st := h.agentState(e); st.PublicKey != e.pub || st.RefreshDue == nil || st.RefreshDue.After(h.now().Add(25*time.Hour)) {
		t.Fatalf("a failed refresh should be retried soon: %q %v", st.PublicKey, st.RefreshDue)
	}

	// The admin is told, in plain words, and the old key kept working.
	var home struct {
		Attention []map[string]any `json:"attention"`
	}
	h.mustCall("GET", "/api/home", nil, &home, 200)
	var card bool
	for _, a := range home.Attention {
		if a["kind"] == "refresh_failed" && a["device_name"] == "Sam's Linux computer" {
			card = true
		}
	}
	if !card {
		t.Fatalf("no attention card: %v", home.Attention)
	}
	var recs []map[string]any
	h.mustCall("GET", "/api/activity", nil, &recs, 200)
	var said bool
	for _, r := range recs {
		if strings.Contains(r["text"].(string), "could not refresh its security") {
			said = true
		}
	}
	if !said {
		t.Fatal("Activity does not mention the failed refresh")
	}

	// A good refresh later clears the card.
	_, again, _ := wg.GenerateKeypair()
	h.mustAgent("POST", "/agent/v1/refresh", e.resp.AuthToken, agent.RefreshRequest{PublicKey: again}, nil, 200)
	h.mustCall("GET", "/api/home", nil, &home, 200)
	for _, a := range home.Attention {
		if a["kind"] == "refresh_failed" {
			t.Fatal("the attention card stayed after a successful refresh")
		}
	}

	// After an hour the old key is retired for good.
	h.advance(61 * time.Minute)
	h.login()
	h.mustAgent("POST", "/agent/v1/refresh/rollback", e.resp.AuthToken, struct{}{}, nil, 409)
	if st := h.agentState(e); st.PublicKey != again {
		t.Fatal("a late rollback changed the key")
	}
}

func TestOnlyManualDevicesAskForARefreshByHand(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	h.addDevice(sam, "ios")
	h.enrollAgent(h.addAgentDevice(sam, "linux").Token)
	h.advance(181 * 24 * time.Hour)
	h.login()
	var home struct {
		Attention []map[string]any `json:"attention"`
	}
	h.mustCall("GET", "/api/home", nil, &home, 200)
	var manual, agentCard int
	for _, a := range home.Attention {
		if a["kind"] == "refresh_due" {
			if a["device_name"] == "Sam's iPhone" {
				manual++
			} else {
				agentCard++
			}
		}
	}
	if manual != 1 || agentCard != 0 {
		t.Fatalf("manual %d, agent %d: %v", manual, agentCard, home.Attention)
	}
}
