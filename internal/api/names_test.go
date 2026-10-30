package api

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"lsl-wardline/internal/gateway"
)

// Names for machines: specs/BUSINESS-RULES.md, "Names for machines".

type named struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ShortName       string   `json:"short_name"`
	NameOnNetwork   string   `json:"name_on_network"`
	ShortNameCustom bool     `json:"short_name_custom"`
	Aliases         []string `json:"aliases"`
}

func (h *harness) addNamed(name string) named {
	h.t.Helper()
	var out struct {
		Device named `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "windows", "name": name}, &out, 201)
	var d named
	h.mustCall("GET", "/api/devices/"+out.Device.ID, nil, &d, 200)
	return d
}

func TestAMachineGetsAShortNameFromItsNameAndNumbersFollow(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	if a.ShortName != "office-pc" || a.NameOnNetwork != "office-pc.wardline.internal" || a.ShortNameCustom {
		t.Fatalf("%+v", a)
	}
	// A different friendly name that makes the same short name is numbered.
	b := h.addNamed("Office  PC!")
	if b.ShortName != "office-pc-2" {
		t.Fatalf("%+v", b)
	}
	// "server" is kept for the server.
	c := h.addNamed("Server")
	if c.ShortName != "server-2" {
		t.Fatalf("%+v", c)
	}
}

func TestRenamingFollowsTheShortNameUnlessItWasSetByHand(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	var out named
	h.mustCall("PATCH", "/api/devices/"+a.ID, map[string]string{"name": "Back PC"}, &out, 200)
	var d named
	h.mustCall("GET", "/api/devices/"+a.ID, nil, &d, 200)
	if d.ShortName != "back-pc" {
		t.Fatalf("the short name should follow the rename: %+v", d)
	}
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "files-pc", "aliases": []string{}}, &d, 200)
	if d.ShortName != "files-pc" || !d.ShortNameCustom {
		t.Fatalf("%+v", d)
	}
	h.mustCall("PATCH", "/api/devices/"+a.ID, map[string]string{"name": "Front PC"}, nil, 200)
	h.mustCall("GET", "/api/devices/"+a.ID, nil, &d, 200)
	if d.ShortName != "files-pc" {
		t.Fatalf("a short name set by hand must stay: %+v", d)
	}
	// Clearing it hands control back to the name.
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "", "aliases": []string{}}, &d, 200)
	if d.ShortName != "front-pc" || d.ShortNameCustom {
		t.Fatalf("%+v", d)
	}
}

func TestExtraNamesAndTheRulesTheyFollow(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	b := h.addNamed("Studio Mac")

	var d named
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "", "aliases": []string{"files", "nas"}}, &d, 200)
	if strings.Join(d.Aliases, ",") != "files,nas" {
		t.Fatalf("%+v", d)
	}

	refused := map[string][]string{
		"another machine's name":      {"studio-mac"},
		"another machine's extra":     {"files"},
		"the server's name":           {"server"},
		"a reserved name":             {"localhost"},
		"capital letters":             {"Files2"},
		"a dot":                       {"a.b"},
		"the same name twice":         {"dup", "dup"},
		"more than ten extra names":   {"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11"},
		"its own short name as extra": {"studio-mac"},
	}
	for why, aliases := range refused {
		var e map[string]string
		h.mustCall("PUT", "/api/devices/"+b.ID+"/names", map[string]any{"short_name": "", "aliases": aliases}, &e, 400)
		if e["error"] == "" {
			t.Errorf("%s: no message", why)
		}
	}
	// A short name typed by hand that is taken is refused, never numbered.
	var e map[string]string
	h.mustCall("PUT", "/api/devices/"+b.ID+"/names", map[string]any{"short_name": "office-pc", "aliases": []string{}}, &e, 400)
	if e["error"] == "" {
		t.Error("a taken short name must be refused with a message")
	}
	// The machine's own current names are not "taken" by itself.
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "office-pc", "aliases": []string{"files"}}, &d, 200)

	h.mustCall("PUT", "/api/devices/nonexistent/names", map[string]any{"short_name": "", "aliases": []string{}}, nil, 404)
	h.hc.Jar = nil
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "", "aliases": []string{}}, nil, 401)
}

func TestNameChangesAreWrittenToActivityInPlainWords(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "", "aliases": []string{"files"}}, nil, 200)
	var text string
	if err := h.st.DB().QueryRow(`SELECT plain_text FROM audit_events WHERE action='device.names_changed'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Office PC") || !strings.Contains(text, "files") {
		t.Fatalf("activity says %q", text)
	}
	h.mustCall("PATCH", "/api/devices/"+a.ID, map[string]string{"name": "Back PC"}, nil, 200)
	if err := h.st.DB().QueryRow(`SELECT plain_text FROM audit_events WHERE action='device.renamed'`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "back-pc") {
		t.Fatalf("a rename should say what the name on the network is now: %q", text)
	}
}

func TestThePreviewSaysWhatNameAMachineWillGet(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	h.addNamed("Office PC")
	var p struct {
		ShortName string `json:"short_name"`
		Full      string `json:"name_on_network"`
		Wanted    string `json:"wanted"`
		Numbered  bool   `json:"numbered"`
	}
	h.mustCall("GET", "/api/names/preview?name="+url.QueryEscape("Studio Mac"), nil, &p, 200)
	if p.ShortName != "studio-mac" || p.Numbered || p.Full != "studio-mac.wardline.internal" {
		t.Fatalf("%+v", p)
	}
	h.mustCall("GET", "/api/names/preview?name="+url.QueryEscape("office pc"), nil, &p, 200)
	if p.ShortName != "office-pc-2" || !p.Numbered || p.Wanted != "office-pc" {
		t.Fatalf("%+v", p)
	}
	h.mustCall("GET", "/api/names/preview?name=", nil, &p, 200)
	if p.ShortName != "machine" {
		t.Fatalf("%+v", p)
	}
}

func TestRemovingAMachineFreesItsNames(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "", "aliases": []string{"files"}}, nil, 200)
	h.mustCall("DELETE", "/api/devices/"+a.ID, nil, nil, 200)
	b := h.addNamed("Office-PC") // a different display name that makes the same short name
	if b.ShortName != "office-pc" {
		t.Fatalf("the name should be free again: %+v", b)
	}
	c := h.addNamed("Another")
	h.mustCall("PUT", "/api/devices/"+c.ID+"/names", map[string]any{"short_name": "", "aliases": []string{"files"}}, nil, 200)
}

func (h *harness) serverID() string {
	h.t.Helper()
	var id string
	if err := h.st.DB().QueryRow(`SELECT id FROM servers ORDER BY rowid LIMIT 1`).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func TestNamesAreSentToTheServerAndMachinesOnlyWhileMachinesCanReachEachOther(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	ctx := context.Background()
	e := h.enrollAgent(h.addAgentDevice(h.person("Sam"), "linux").Token)
	a := h.addNamed("Office PC")
	h.mustCall("PUT", "/api/devices/"+a.ID+"/names", map[string]any{"short_name": "", "aliases": []string{"files"}}, nil, 200)

	// Off (the default): nobody is told any name.
	st, err := h.api.buildState(ctx, h.serverID(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Names) != 0 || st.NamesDomain != "" {
		t.Fatalf("names must not be sent while machines are kept apart: %+v", st)
	}
	if as := h.agentState(e); as.NamesDomain != "" || as.NamesServer != "" {
		t.Fatalf("a machine must not be told to use names while they are off: %+v", as)
	}

	h.mustCall("PUT", "/api/network/devices-reach-each-other", map[string]bool{"enabled": true}, nil, 200)
	st, err = h.api.buildState(ctx, h.serverID(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if st.NamesDomain != "wardline.internal" {
		t.Fatalf("%+v", st)
	}
	got := map[string]string{}
	for _, n := range st.Names {
		got[n.Name] = n.IP
	}
	var office string
	h.st.DB().QueryRow(`SELECT tunnel_ip FROM devices WHERE id=?`, a.ID).Scan(&office)
	srvIP := strings.Split(st.Address, "/")[0]
	if got["office-pc"] != office || got["files"] != office || got["server"] != srvIP {
		t.Fatalf("names %v, want office-pc and files -> %s, server -> %s", got, office, srvIP)
	}
	as := h.agentState(e)
	if as.NamesDomain != "wardline.internal" || as.NamesServer != srvIP {
		t.Fatalf("%+v (server %s)", as, srvIP)
	}

	// Changing a name changes what is sent, and the version goes up so everyone hears of it.
	before := h.api.version(ctx)
	h.mustCall("PATCH", "/api/devices/"+a.ID, map[string]string{"name": "Back PC"}, nil, 200)
	if h.api.version(ctx) <= before {
		t.Fatal("renaming a machine must raise the state version so the server updates its names")
	}
}

func TestOlderMachinesGetNamesWhenTheServerStarts(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	b := h.addNamed("Office PC")
	if _, err := h.st.DB().Exec(`UPDATE devices SET short_name=NULL`); err != nil {
		t.Fatal(err)
	}
	if err := h.api.backfillShortNames(context.Background()); err != nil {
		t.Fatal(err)
	}
	var da, db named
	h.mustCall("GET", "/api/devices/"+a.ID, nil, &da, 200)
	h.mustCall("GET", "/api/devices/"+b.ID, nil, &db, 200)
	if da.ShortName != "office-pc" || db.ShortName != "office-pc-2" {
		t.Fatalf("%q %q", da.ShortName, db.ShortName)
	}
}

var _ = gateway.NameRecord{}
