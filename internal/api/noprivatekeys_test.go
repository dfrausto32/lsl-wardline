package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"lsl-wardline/internal/auth"
	"lsl-wardline/internal/wg"
)

func wgKeypair(t *testing.T) (string, string, error) {
	t.Helper()
	return wg.GenerateKeypair()
}

var cfgPrivateRe = regexp.MustCompile(`PrivateKey = (\S+)`)
var cfgPSKRe = regexp.MustCompile(`PresharedKey = (\S+)`)

// TestNoPrivateKeysAnywhere is acceptance item A6: no private key appears in
// the database, the logs, or any API response, except the one-time device code
// response, which is not stored.
func TestNoPrivateKeysAnywhere(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	g, fake := h.connectedServer(dir)
	ctx := context.Background()

	// Enable two-step sign-in so its secret exists too.
	var begin struct{ Secret string }
	h.mustCall("POST", "/api/totp/begin", nil, &begin, 200)
	h.mustCall("POST", "/api/totp/confirm", map[string]string{"code": auth.TOTPCode(begin.Secret, h.now())}, nil, 200)

	sam := h.person("Sam")
	var secrets []string // private keys and secrets that must not be found anywhere
	var psks []string    // preshared keys: encrypted at rest, and sent only to the authenticated server
	var responses [][]byte

	capture := func(method, path string) {
		req, _ := http.NewRequest(method, h.ts.URL+path, nil)
		res, err := h.hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		responses = append(responses, b)
	}

	for _, platform := range []string{"ios", "android", "windows", "macos", "linux"} {
		a := h.addDevice(sam, platform)
		priv := cfgPrivateRe.FindStringSubmatch(a.Config)
		psk := cfgPSKRe.FindStringSubmatch(a.Config)
		if priv == nil || psk == nil {
			t.Fatalf("config for %s has no keys:\n%s", platform, a.Config)
		}
		secrets = append(secrets, priv[1])
		psks = append(psks, psk[1])
		capture("GET", "/api/devices/"+a.Device["id"].(string))
	}
	secrets = append(secrets, begin.Secret)

	// The server's own private key lives in its identity file and nowhere else.
	idRaw, err := os.ReadFile(dir + "/identity.json")
	if err != nil {
		t.Fatal(err)
	}
	var id struct {
		PrivateKey string `json:"private_key"`
	}
	json.Unmarshal(idRaw, &id)
	if id.PrivateKey == "" {
		t.Fatal("server identity has no key to look for")
	}
	secrets = append(secrets, id.PrivateKey)

	// Let the server apply the state and report, and remove a device, so those paths are exercised too.
	g.Sync(ctx)
	peers, _ := fake.Peers(ctx, "wardline0")
	fake.SimulateHandshake("wardline0", peers[0].PublicKey, 1, 2, "198.51.100.4:1")
	g.ReportNow(ctx)
	var devs []map[string]any
	h.mustCall("GET", "/api/devices", nil, &devs, 200)
	h.mustCall("DELETE", "/api/devices/"+devs[0]["id"].(string), nil, nil, 200)

	for _, p := range []string{"/api/state", "/api/home", "/api/devices", "/api/people", "/api/servers", "/api/network", "/api/activity", "/api/activity.csv"} {
		capture("GET", p)
	}
	// What the server itself is sent. It legitimately carries preshared keys, never private keys.
	var sid string
	h.st.DB().QueryRow(`SELECT id FROM servers LIMIT 1`).Scan(&sid)
	st, err := h.api.buildState(ctx, sid, 1)
	if err == nil {
		b, _ := json.Marshal(st)
		for _, s := range secrets {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("desired state sent to the server contains a private key (%.6s...)", s)
			}
		}
	}

	all := append(append([]string{}, secrets...), psks...)

	// 1. API responses
	for i, body := range responses {
		for _, s := range all {
			if bytes.Contains(body, []byte(s)) {
				t.Errorf("response %d contains a secret (%.6s...)", i, s)
			}
		}
	}

	// 2. Logs
	for _, s := range all {
		if strings.Contains(h.logs.String(), s) {
			t.Errorf("logs contain a secret (%.6s...)", s)
		}
	}

	// 3. The database files, including the write-ahead log, and a dump of every column.
	h.st.DB().Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
	for _, f := range []string{h.dbPath, h.dbPath + "-wal"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, s := range all {
			if bytes.Contains(raw, []byte(s)) {
				t.Errorf("%s contains a secret (%.6s...)", f, s)
			}
		}
	}
	rows, err := h.st.DB().Query(`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	type tc struct{ table, col string }
	var cols []tc
	for rows.Next() {
		var c tc
		rows.Scan(&c.table, &c.col)
		cols = append(cols, c)
	}
	rows.Close()
	for _, c := range cols {
		for _, s := range all {
			var n int
			q := `SELECT count(*) FROM "` + c.table + `" WHERE CAST("` + c.col + `" AS TEXT) LIKE ?`
			h.st.DB().QueryRow(q, "%"+s+"%").Scan(&n)
			if n > 0 {
				t.Errorf("%s.%s contains a secret (%.6s...)", c.table, c.col, s)
			}
		}
	}

	// 4. Preshared keys exist, but only as ciphertext.
	var n int
	h.st.DB().QueryRow(`SELECT count(*) FROM preshared_keys`).Scan(&n)
	if n == 0 {
		t.Fatal("expected encrypted preshared keys to exist for the remaining devices")
	}
}
