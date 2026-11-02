package api

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// Task 6.7: no private key appears in the membership log, its projection or the logs. The log holds public
// keys and signatures only; the authority's own key lives in the control plane's sealed settings.
func TestNoPrivateKeyIsInTheMembershipLog(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	sam := h.person("Sam")

	secrets := map[string]string{}
	for _, platform := range []string{"ios", "windows", "macos"} {
		a := h.addDevice(sam, platform)
		secrets["device key ("+platform+")"] = cfgPrivateRe.FindStringSubmatch(a.Config)[1]
	}
	ac := h.addAgentDevice(sam, "linux")
	ae := h.enrollAgent(ac.Token)
	secrets["agent key"] = ae.priv
	secrets["agent enrollment code"] = ac.Token
	secrets["agent access token"] = ae.resp.AuthToken

	// The authority's own signing key, in every spelling it could leak in.
	priv := h.api.d.SigningKey
	seed := priv.Seed()
	secrets["authority seed (base64)"] = base64.StdEncoding.EncodeToString(seed)
	secrets["authority seed (base64 url)"] = base64.RawURLEncoding.EncodeToString(seed)
	secrets["authority seed (hex)"] = hex.EncodeToString(seed)
	secrets["authority private key (base64)"] = base64.StdEncoding.EncodeToString(priv)
	secrets["authority private key (hex)"] = hex.EncodeToString(priv)

	// Write more of the log: a removal and a refreshed key.
	h.mustCall("DELETE", "/api/devices/"+ac.Device["id"].(string), nil, nil, 200)

	check := func(where string, haystack []byte) {
		for name, s := range secrets {
			if s != "" && bytes.Contains(haystack, []byte(s)) {
				t.Errorf("%s contains the %s", where, name)
			}
		}
	}

	// The stored entries, their signatures and the projection, column by column.
	for _, table := range []string{"groups", "membership_entries", "members"} {
		cols, err := h.st.DB().Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for cols.Next() {
			var n string
			cols.Scan(&n)
			names = append(names, n)
		}
		cols.Close()
		if len(names) == 0 {
			t.Fatalf("table %s does not exist", table)
		}
		for _, c := range names {
			rows, err := h.st.DB().Query(`SELECT CAST("` + c + `" AS BLOB) FROM "` + table + `"`)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var b []byte
				rows.Scan(&b)
				check(table+"."+c, b)
			}
			rows.Close()
		}
	}

	// Each entry as it would travel (this is also what a bundle or the Edge could be sent), and the logs.
	l, _ := h.groupLog()
	for _, e := range l.Entries() {
		wire, err := e.Wire()
		if err != nil {
			t.Fatal(err)
		}
		check("entry "+e.ID[:8], wire)
		// The only keys in an entry are public ones, which are 32 bytes, as are the private seeds; the point
		// of this test is that the sealed signing seed is never among them.
		if strings.Contains(string(wire), `"seed"`) || strings.Contains(string(wire), "private") {
			t.Errorf("entry %s has a field that looks like a private key", e.ID[:8])
		}
	}
	check("the logs", h.logs.Bytes())

	// And the signing key really does sign what the log says it signs: the public half is in the genesis entry.
	st := l.State()
	if st.Members[st.Authority].SigningPub != base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)) {
		t.Fatal("the authority's public key is not the one in the log")
	}
}
