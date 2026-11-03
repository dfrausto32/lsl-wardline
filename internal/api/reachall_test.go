package api

import (
	"context"
	"strings"
	"testing"
)

// Reach-all permission (task 6.9): specs/BUSINESS-RULES.md, "Reach-all permission".

func (h *harness) setReachAll(id string, on bool, want int) {
	h.t.Helper()
	h.mustCall("PUT", "/api/devices/"+id+"/reach-all", map[string]bool{"enabled": on}, nil, want)
}

func (h *harness) tunnelIP(id string) string {
	h.t.Helper()
	var ip string
	if err := h.st.DB().QueryRow(`SELECT tunnel_ip FROM devices WHERE id=?`, id).Scan(&ip); err != nil {
		h.t.Fatal(err)
	}
	return ip
}

func TestNoMachineMayReachEveryoneUntilAnAdminSaysSoAndTheServerHearsAtOnce(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	ctx := context.Background()
	edge := h.addNamed("Edge")
	other := h.addNamed("Office PC")

	var d map[string]any
	h.mustCall("GET", "/api/devices/"+edge.ID, nil, &d, 200)
	if d["can_reach_all"] != false {
		t.Fatalf("the permission is off by default: %v", d["can_reach_all"])
	}
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if p, set := fake.Relay("wardline0"); !set || p.All || len(p.ReachAll) != 0 {
		t.Fatalf("nothing relayed yet: %+v", p)
	}

	var out map[string]any
	h.mustCall("PUT", "/api/devices/"+edge.ID+"/reach-all", map[string]bool{"enabled": true}, &out, 200)
	if out["can_reach_all"] != true {
		t.Fatalf("%v", out)
	}
	h.mustCall("GET", "/api/devices/"+edge.ID, nil, &d, 200)
	if d["can_reach_all"] != true {
		t.Fatalf("%v", d)
	}
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := fake.Relay("wardline0")
	if p.All || len(p.ReachAll) != 1 || p.ReachAll[0].String() != h.tunnelIP(edge.ID) {
		t.Fatalf("only the Edge may reach everyone: %+v (Edge %s, other %s)", p, h.tunnelIP(edge.ID), h.tunnelIP(other.ID))
	}
	// The other machine did not get it, and the network setting stayed off.
	h.mustCall("GET", "/api/devices/"+other.ID, nil, &d, 200)
	if d["can_reach_all"] != false {
		t.Fatalf("%v", d)
	}
	var net map[string]any
	h.mustCall("GET", "/api/network", nil, &net, 200)
	if net["devices_reach_each_other"] != false || net["names_active"] != false {
		t.Fatalf("the permission must not turn on the network setting or the names: %v", net)
	}

	h.setReachAll(edge.ID, false, 200)
	if _, err := g.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if p, _ := fake.Relay("wardline0"); len(p.ReachAll) != 0 {
		t.Fatalf("taking it away must stop it: %+v", p)
	}
}

func TestAnyNumberOfMachinesMayHoldIt(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	a, b, c := h.addNamed("Edge one"), h.addNamed("Edge two"), h.addNamed("Office PC")
	h.setReachAll(a.ID, true, 200)
	h.setReachAll(b.ID, true, 200)
	if _, err := g.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ := fake.Relay("wardline0")
	got := map[string]bool{}
	for _, x := range p.ReachAll {
		got[x.String()] = true
	}
	if len(got) != 2 || !got[h.tunnelIP(a.ID)] || !got[h.tunnelIP(b.ID)] || got[h.tunnelIP(c.ID)] {
		t.Fatalf("%+v", p)
	}
}

func TestARemovedMachineLosesItAndCannotBeGivenIt(t *testing.T) {
	h := newHarness(t)
	g, fake := h.connectedServer(t.TempDir())
	e := h.addNamed("Edge")
	h.setReachAll(e.ID, true, 200)
	h.mustCall("DELETE", "/api/devices/"+e.ID, nil, nil, 200)
	if _, err := g.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p, _ := fake.Relay("wardline0"); len(p.ReachAll) != 0 {
		t.Fatalf("a removed machine must not keep the permission: %+v", p)
	}
	h.setReachAll(e.ID, true, 404)
	h.setReachAll("no-such-machine", true, 404)
}

func TestGrantingAndRemovingItIsWrittenToActivityInPlainWords(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	e := h.addNamed("Edge")
	h.setReachAll(e.ID, true, 200)
	h.setReachAll(e.ID, false, 200)
	rows, err := h.st.DB().Query(`SELECT plain_text FROM audit_events WHERE action='device.reach_all_changed' ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var texts []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		texts = append(texts, s)
	}
	if len(texts) != 2 || !strings.Contains(texts[0], "Edge") || !strings.Contains(texts[0], "reach every other machine") ||
		!strings.Contains(texts[1], "no longer") {
		t.Fatalf("activity says %q", texts)
	}
}

func TestSettingItToWhatItAlreadyIsChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	e := h.addNamed("Edge")
	h.setReachAll(e.ID, false, 200)
	var n int
	h.st.DB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='device.reach_all_changed'`).Scan(&n)
	if n != 0 {
		t.Fatalf("no change, so nothing to record (%d events)", n)
	}
	h.setReachAll(e.ID, true, 200)
	h.setReachAll(e.ID, true, 200)
	h.st.DB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='device.reach_all_changed'`).Scan(&n)
	if n != 1 {
		t.Fatalf("recorded once, not twice (%d events)", n)
	}
}

func TestOnlyASignedInAdminCanGiveIt(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	e := h.addNamed("Edge")
	h.hc.Jar = nil
	h.setReachAll(e.ID, true, 401)
}

func TestTheEdgeIsToldWhichMachinesHoldIt(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	e := h.addNamed("Edge")
	h.addNamed("Office PC")
	_, token := h.makeToken("Edge key")

	_, ms := h.members(token)
	for _, m := range ms {
		if m["reaches_all"] != false {
			t.Fatalf("everyone starts without it, the server too: %v", m)
		}
	}
	h.setReachAll(e.ID, true, 200)
	_, ms = h.members(token)
	if byLabel(ms, "edge")["reaches_all"] != true || byLabel(ms, "office-pc")["reaches_all"] != false {
		t.Fatalf("%v", ms)
	}
	// The roles list is the contract's own, so it does not change.
	if roles := byLabel(ms, "edge")["roles"].([]any); len(roles) != 0 {
		t.Fatalf("roles must stay as the contract has them: %v", roles)
	}
}
