//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// agentSteps runs after the manual-device steps, in the same lab: two computers set themselves up
// with the real agent, one refreshes its own key while traffic keeps flowing, and one is removed.
func agentSteps(t *testing.T, l *lab, serverTunnelIP string) {
	var jo struct {
		ID string `json:"id"`
	}
	l.must("POST", "/api/people", map[string]string{"name": "Jo"}, &jo, 201)

	type agentDev struct{ name, id string }
	var devs []agentDev
	for _, name := range []string{"agent-1", "agent-2"} {
		var created struct {
			Device  map[string]any    `json:"device"`
			Token   string            `json:"token"`
			Install map[string]string `json:"install"`
		}
		l.must("POST", "/api/devices", map[string]any{"person_id": jo.ID, "platform": "linux", "agent": true}, &created, 201)
		if created.Token == "" || !strings.Contains(created.Install["command"], created.Token) {
			t.Fatalf("no setup code: %+v", created)
		}
		l.writeFile(name+".token", created.Token)
		l.writeFile(name+".target", serverTunnelIP+"\n")
		devs = append(devs, agentDev{name, created.Device["id"].(string)})
	}

	for _, d := range devs {
		d := d
		l.eventually(d.name+" to connect", 90*time.Second, func() bool { return l.deviceStatus(d.id) == "connected" })
		l.eventually(d.name+" to pass traffic", 30*time.Second, func() bool {
			ts, ok := l.lastOK(d.name)
			return ok && time.Since(ts) < 4*time.Second
		})
	}
	t.Log("both agent computers connected")

	publicKey := func(id string) string {
		var d struct {
			Advanced struct {
				PublicKey string `json:"public_key"`
			} `json:"advanced"`
		}
		l.must("GET", "/api/devices/"+id, nil, &d, 200)
		return d.Advanced.PublicKey
	}

	// Silent refresh: ask agent-1 to refresh now. Its key changes at the control plane and traffic carries on.
	before := publicKey(devs[0].id)
	if err := os.WriteFile(filepath.Join(l.dir, "state-agent-1", "refresh-now"), []byte("now\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l.eventually("agent-1 to take a new key", 60*time.Second, func() bool { return publicKey(devs[0].id) != before })
	refreshedAt := time.Now()
	l.eventually("agent-1 to keep passing traffic on its new key", 40*time.Second, func() bool {
		ts, ok := l.lastOK("agent-1")
		return ok && ts.After(refreshedAt.Add(2*time.Second)) && time.Since(ts) < 4*time.Second
	})
	if st := l.deviceStatus(devs[0].id); st != "connected" {
		t.Fatalf("agent-1 status after refresh = %q", st)
	}
	t.Log("agent-1 refreshed its key and kept working")

	// Removing access: traffic stops, and the computer deletes its tunnel and keys by itself.
	removedAt := time.Now()
	l.must("DELETE", "/api/devices/"+devs[1].id, nil, nil, 200)
	l.eventually("traffic from the removed agent to stop", 15*time.Second, func() bool {
		ts, ok := l.lastOK("agent-2")
		return ok && time.Since(ts) >= 3*time.Second
	})
	t.Logf("removed agent stopped %v after removal", time.Since(removedAt).Round(100*time.Millisecond))
	l.eventually("the removed computer to delete its keys", 30*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(l.dir, "state-agent-2", "agent.json"))
		return err != nil
	})
	if ts, _ := l.lastOK("agent-1"); time.Since(ts) > 5*time.Second {
		t.Fatal("the other agent lost its connection when one was removed")
	}

	// Activity says all of it in plain words.
	var act []struct {
		Text string `json:"text"`
	}
	l.must("GET", "/api/activity", nil, &act, 200)
	var all strings.Builder
	for _, e := range act {
		all.WriteString(e.Text + "\n")
	}
	for _, want := range []string{"Jo's Linux computer was set up", "Jo's Linux computer 2 was set up", "Jo's Linux computer refreshed its security.", "Jo's Linux computer 2 was removed."} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("Activity is missing %q:\n%s", want, all.String())
		}
	}
}
