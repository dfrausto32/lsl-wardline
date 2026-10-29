//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These values exist only for the lab; the test creates the account itself.
const (
	labEmail    = "e2e@lab.test"
	labPassword = "lab-only passphrase 1234"
)

type lab struct {
	t    *testing.T
	base string
	dir  string
	hc   *http.Client
}

func newLab(t *testing.T) *lab {
	jar, _ := cookiejar.New(nil)
	return &lab{t: t, base: env("LAB_CONTROL_URL", "http://control:8080"), dir: env("LAB_DIR", "/lab"), hc: &http.Client{Jar: jar, Timeout: 20 * time.Second}}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (l *lab) call(method, path string, body, out any) int {
	l.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, l.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		req.Header.Set("X-Wardline", "1")
	}
	res, err := l.hc.Do(req)
	if err != nil {
		l.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	if out != nil {
		json.NewDecoder(res.Body).Decode(out)
	}
	return res.StatusCode
}

func (l *lab) must(method, path string, body, out any, want int) {
	l.t.Helper()
	if got := l.call(method, path, body, out); got != want {
		l.t.Fatalf("%s %s: status %d, want %d", method, path, got, want)
	}
}

func (l *lab) eventually(what string, within time.Duration, cond func() bool) {
	l.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	l.t.Fatalf("timed out after %v waiting for %s", within, what)
}

func (l *lab) writeFile(name, content string) {
	l.t.Helper()
	tmp := filepath.Join(l.dir, name+".tmp")
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		l.t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(l.dir, name)); err != nil {
		l.t.Fatal(err)
	}
}

// lastOK returns when the device last got an answer from the server through its tunnel.
func (l *lab) lastOK(dev string) (time.Time, bool) { return l.lastFile(dev + ".lastok") }

// lastFile reads a time recorded by a lab computer.
func (l *lab) lastFile(name string) (time.Time, bool) {
	b, err := os.ReadFile(filepath.Join(l.dir, name))
	if err != nil {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(n, 0), true
}

func (l *lab) deviceStatus(id string) string {
	var d struct {
		Status string `json:"status"`
	}
	if l.call("GET", "/api/devices/"+id, nil, &d) != 200 {
		return ""
	}
	return d.Status
}

var tokenRe = regexp.MustCompile(`WARDLINE_ENROLL_TOKEN=(\S+)`)

func TestLabEndToEnd(t *testing.T) {
	l := newLab(t)

	// 1. The control plane is up and brand new.
	l.eventually("the control plane to start", 90*time.Second, func() bool {
		var st map[string]any
		return l.call("GET", "/api/state", nil, &st) == 200
	})

	// 2. First-run setup, then the address devices will use. A name, as recommended.
	l.must("POST", "/api/setup", map[string]string{"network_name": "Lab", "email": labEmail, "password": labPassword}, nil, 201)
	l.must("PUT", "/api/network/server-address", map[string]string{"address": "server-a"}, nil, 200)
	var net struct {
		Advanced struct {
			AddressRange string `json:"address_range"`
		} `json:"advanced"`
	}
	l.must("GET", "/api/network", nil, &net, 200)
	prefix := netip.MustParsePrefix(net.Advanced.AddressRange)
	serverTunnelIP := prefix.Masked().Addr().Next().String()

	// 3. Add the server: the install code goes to the lab's server-a container.
	var created struct {
		Install map[string]string `json:"install"`
	}
	l.must("POST", "/api/servers", map[string]string{"name": "server-a"}, &created, 201)
	m := tokenRe.FindStringSubmatch(created.Install["docker"])
	if m == nil {
		t.Fatal("the install command has no code in it")
	}
	l.writeFile("token-server-a", m[1])
	l.eventually("the server to check in", 60*time.Second, func() bool {
		var servers []struct {
			Status string `json:"status"`
		}
		l.call("GET", "/api/servers", nil, &servers)
		return len(servers) == 1 && servers[0].Status == "online"
	})
	t.Log("server is online")

	// 4. Add two devices through the API and hand each its generated config.
	var sam, alex struct {
		ID string `json:"id"`
	}
	l.must("POST", "/api/people", map[string]string{"name": "Sam"}, &sam, 201)
	l.must("POST", "/api/people", map[string]string{"name": "Alex"}, &alex, 201)
	add := func(person, dev string) string {
		var out struct {
			Device map[string]any `json:"device"`
			Config string         `json:"config"`
		}
		l.must("POST", "/api/devices", map[string]string{"person_id": person, "platform": "linux"}, &out, 201)
		if !strings.Contains(out.Config, "Endpoint = server-a:51820") {
			t.Fatalf("config does not point at the server:\n%s", out.Config)
		}
		l.writeFile(dev+".conf", out.Config)
		l.writeFile(dev+".target", serverTunnelIP+"\n")
		return out.Device["id"].(string)
	}
	id1 := add(sam.ID, "device-linux-1")
	id2 := add(alex.ID, "device-linux-2")

	// 5. Both devices connect with the generated config; the handshake shows in the API.
	for _, id := range []string{id1, id2} {
		l.eventually("device "+id+" to show as connected", 60*time.Second, func() bool { return l.deviceStatus(id) == "connected" })
	}
	t.Log("both devices connected")
	for _, dev := range []string{"device-linux-1", "device-linux-2"} {
		l.eventually(dev+" to pass traffic", 30*time.Second, func() bool {
			ts, ok := l.lastOK(dev)
			return ok && time.Since(ts) < 4*time.Second
		})
	}

	// 6. Remove access for the first device. Traffic must stop within 10 seconds.
	removedAt := time.Now()
	l.must("DELETE", "/api/devices/"+id1, nil, nil, 200)
	l.eventually("traffic from the removed device to stop", 10*time.Second, func() bool {
		ts, ok := l.lastOK("device-linux-1")
		return ok && time.Since(ts) >= 3*time.Second
	})
	t.Logf("traffic stopped %v after removal", time.Since(removedAt).Round(100*time.Millisecond))

	// 7. It stays stopped, and the other device is unaffected.
	time.Sleep(5 * time.Second)
	ts, _ := l.lastOK("device-linux-1")
	if ts.After(removedAt.Add(2 * time.Second)) {
		t.Fatalf("removed device still got traffic through at %v", ts)
	}
	l.eventually("the other device to keep working", 10*time.Second, func() bool {
		ts, ok := l.lastOK("device-linux-2")
		return ok && time.Since(ts) < 4*time.Second
	})
	if st := l.deviceStatus(id2); st != "connected" {
		t.Fatalf("other device status = %q", st)
	}

	// 8. Everything is in Activity, in plain words.
	var act []struct {
		Text string `json:"text"`
	}
	l.must("GET", "/api/activity", nil, &act, 200)
	var all strings.Builder
	for _, e := range act {
		all.WriteString(e.Text + "\n")
	}
	for _, want := range []string{"Sam's Linux computer was removed.", "Alex's Linux computer connected for the first time.", "set up the private network"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("Activity is missing %q:\n%s", want, all.String())
		}
	}
	fmt.Println(all.String())

	// 9. Computers that set themselves up with the agent: connect, refresh their own key, and clean up when removed.
	agentSteps(t, l, serverTunnelIP)
}
