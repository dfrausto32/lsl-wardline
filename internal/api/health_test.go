package api

import (
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/agent"
)

func TestAgentReportShowsTheDeviceAsConnectedAndNamesProblemsInPlainTerms(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	c := h.addAgentDevice(h.person("Sam"), "linux")
	id := c.Device["id"].(string)
	e := h.enrollAgent(c.Token)

	h.mustAgent("POST", "/agent/v1/report", "", agent.Report{DeviceID: id}, nil, 401)

	now := h.now()
	h.mustAgent("POST", "/agent/v1/report", e.resp.AuthToken, agent.Report{DeviceID: id, Time: now, AgentVersion: "1.2.3", TunnelUp: true, LastHandshake: now.Add(-20 * time.Second)}, nil, 204)

	var d map[string]any
	h.mustCall("GET", "/api/devices/"+id, nil, &d, 200)
	if d["status"] != "connected" {
		t.Fatalf("status %v", d["status"])
	}
	agentInfo, _ := d["agent"].(map[string]any)
	if agentInfo["version"] != "1.2.3" || agentInfo["problem"] != "" {
		t.Fatalf("%v", d["agent"])
	}
	// The first connection is written to Activity once, however many times it is reported.
	h.mustAgent("POST", "/agent/v1/report", e.resp.AuthToken, agent.Report{DeviceID: id, Time: now, TunnelUp: true, LastHandshake: now}, nil, 204)
	var recs []map[string]any
	h.mustCall("GET", "/api/activity", nil, &recs, 200)
	n := 0
	for _, r := range recs {
		if strings.Contains(r["text"].(string), "connected for the first time") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("first connection written %d times", n)
	}

	// A problem becomes an attention card with a code the web app turns into words.
	h.mustAgent("POST", "/agent/v1/report", e.resp.AuthToken, agent.Report{DeviceID: id, Time: now, Problem: agent.ProblemNameNotFound}, nil, 204)
	var home struct {
		Attention []map[string]any `json:"attention"`
	}
	h.mustCall("GET", "/api/home", nil, &home, 200)
	var found bool
	for _, a := range home.Attention {
		if a["kind"] == "device_problem" && a["problem"] == agent.ProblemNameNotFound && a["device_name"] == "Sam's Linux computer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no problem card: %v", home.Attention)
	}

	// It clears when the device says it is fine, and an old report is not trusted.
	h.mustAgent("POST", "/agent/v1/report", e.resp.AuthToken, agent.Report{DeviceID: id, Time: now}, nil, 204)
	h.mustCall("GET", "/api/home", nil, &home, 200)
	for _, a := range home.Attention {
		if a["kind"] == "device_problem" {
			t.Fatal("the card stayed after the problem went away")
		}
	}
	h.mustAgent("POST", "/agent/v1/report", e.resp.AuthToken, agent.Report{DeviceID: id, Time: now, Problem: agent.ProblemServerUnreachable}, nil, 204)
	h.advance(30 * time.Minute)
	h.login()
	h.mustCall("GET", "/api/home", nil, &home, 200)
	for _, a := range home.Attention {
		if a["kind"] == "device_problem" {
			t.Fatal("an old report still raises an alarm")
		}
	}
}

func TestAgentReportCannotSpeakForAnotherDeviceOrTheFuture(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")
	a := h.addAgentDevice(sam, "linux")
	b := h.addAgentDevice(sam, "windows")
	ea := h.enrollAgent(a.Token)
	h.enrollAgent(b.Token)
	bid := b.Device["id"].(string)

	// The device in the token is the device that is reported on, whatever the body says.
	now := h.now()
	h.mustAgent("POST", "/agent/v1/report", ea.resp.AuthToken, agent.Report{DeviceID: bid, Time: now, TunnelUp: true, LastHandshake: now, Problem: agent.ProblemServerUnreachable}, nil, 204)
	var d map[string]any
	h.mustCall("GET", "/api/devices/"+bid, nil, &d, 200)
	if d["status"] != "waiting" {
		t.Fatalf("another device's report changed this one: %v", d["status"])
	}

	// A handshake in the far future is clamped to now.
	h.mustAgent("POST", "/agent/v1/report", ea.resp.AuthToken, agent.Report{Time: now, TunnelUp: true, LastHandshake: now.Add(48 * time.Hour)}, nil, 204)
	h.mustCall("GET", "/api/devices/"+a.Device["id"].(string), nil, &d, 200)
	seen, _ := time.Parse(time.RFC3339, d["last_seen_at"].(string))
	if seen.After(now.Add(2 * time.Minute)) {
		t.Fatalf("last seen %v is in the future", seen)
	}
}
