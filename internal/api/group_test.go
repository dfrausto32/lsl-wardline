package api

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"testing"

	"lsl-wardline/internal/membership"
	"lsl-wardline/internal/wg"
)

// A managed group records its membership in the signed log, without changing anything else
// (specs/MEMBERSHIP-AND-MODES.md section 5.4).

func (h *harness) groupLog() (*membership.Log, string) {
	h.t.Helper()
	var gid string
	if err := h.st.DB().QueryRow(`SELECT id FROM groups`).Scan(&gid); err != nil {
		h.t.Fatalf("no group: %v", err)
	}
	rows, err := h.st.DB().Query(`SELECT body FROM membership_entries WHERE group_id=?`, gid)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	l := membership.NewLog(gid)
	var entries []*membership.Entry
	for rows.Next() {
		var b []byte
		rows.Scan(&b)
		e, err := membership.ParseWire(b)
		if err != nil {
			h.t.Fatal(err)
		}
		entries = append(entries, e)
	}
	for _, e := range entries {
		if r := l.Add(e); r.Status != membership.StatusApplied {
			h.t.Fatalf("a stored entry is %s: %s", r.Status, r.Reason)
		}
	}
	return l, gid
}

func (h *harness) entryCount() int {
	var n int
	h.st.DB().QueryRow(`SELECT count(*) FROM membership_entries`).Scan(&n)
	return n
}

func TestSettingUpANetworkStartsAManagedGroupWithTheControlPlaneAsAuthority(t *testing.T) {
	h := newHarness(t)
	h.setup()
	l, gid := h.groupLog()
	st := l.State()
	if st.Mode != membership.ModeAuthority || st.Name != "Home" || st.GroupID != gid {
		t.Fatalf("%+v", st)
	}
	pub := h.api.d.SigningKey.Public().(ed25519.PublicKey)
	if st.Authority != membership.MemberID(pub) {
		t.Fatal("the control plane's signing key must be the authority")
	}
	var mode, name string
	h.st.DB().QueryRow(`SELECT mode, name FROM groups`).Scan(&mode, &name)
	if mode != "authority" || name != "Home" {
		t.Fatalf("group row %s %s", mode, name)
	}
}

func TestAddingAndRemovingMachinesIsWrittenToTheLog(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	b := h.addNamed("Studio Mac")
	l, _ := h.groupLog()
	st := l.State()
	for _, id := range []string{a.ID, b.ID} {
		if !st.Active(id) {
			t.Fatalf("%s is not in the log", id)
		}
	}
	if st.Members[a.ID].Name != "Office PC" || st.Members[a.ID].HostnameLabel != "office-pc" {
		t.Fatalf("%+v", st.Members[a.ID])
	}
	if st.Members[a.ID].WGPub == "" {
		t.Fatal("a phone or laptop set up by hand has its network key from the start")
	}
	h.mustCall("DELETE", "/api/devices/"+a.ID, nil, nil, 200)
	l, _ = h.groupLog()
	if l.State().Active(a.ID) || !l.State().Active(b.ID) {
		t.Fatal("removing a machine must remove it from the log, and only it")
	}
	if !l.State().Members[a.ID].Removed {
		t.Fatal("its record stays, marked removed")
	}
}

func TestTheServerIsAMemberWithTheGatewayRole(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	l, _ := h.groupLog()
	var found bool
	for _, m := range l.State().ActiveMembers() {
		if m.ID == h.serverID() {
			found = true
			if !m.Roles.Gateway {
				t.Fatal("a server is a node with the gateway role")
			}
		}
	}
	if !found {
		t.Fatal("the server is not in the log once it has enrolled")
	}
}

func TestAComputerThatMakesItsOwnKeyGetsItInTheLogWhenItEnrolls(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	c := h.addAgentDevice(h.person("Sam"), "linux")
	id := c.Device["id"].(string)
	l, _ := h.groupLog()
	if m := l.State().Members[id]; m == nil || m.WGPub != "" {
		t.Fatalf("before it enrolls it is a member with no key: %+v", m)
	}
	e := h.enrollAgent(c.Token)
	l, _ = h.groupLog()
	if got := l.State().Members[id].WGPub; got != e.pub {
		t.Fatalf("log has key %q, want %q", got, e.pub)
	}
}

func TestARefreshedKeyIsWrittenToTheLog(t *testing.T) {
	h := newHarness(t)
	g, _ := h.connectedServer(t.TempDir())
	_ = g
	e := h.enrollAgent(h.addAgentDevice(h.person("Sam"), "linux").Token)
	_, newPub, err := wg.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	h.mustAgent("POST", "/agent/v1/refresh", e.resp.AuthToken, map[string]string{"public_key": newPub}, nil, 200)
	l, _ := h.groupLog()
	if got := l.State().Members[e.resp.DeviceID].WGPub; got != newPub {
		t.Fatalf("log has %q, want the refreshed key %q", got, newPub)
	}
}

func TestWritingTheLogTwiceChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	h.addNamed("Office PC")
	before := h.entryCount()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		tx, _ := h.st.DB().BeginTx(ctx, nil)
		if err := h.api.reconcileGroup(ctx, tx); err != nil {
			t.Fatal(err)
		}
		tx.Commit()
	}
	if h.entryCount() != before {
		t.Fatalf("%d entries became %d: reconciling must be idempotent", before, h.entryCount())
	}
}

func TestMachinesThatExistedBeforeTheLogGetRecordedWhenTheServerStarts(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	// Pretend this database was made by an older version: no group, no entries.
	for _, q := range []string{`DELETE FROM members`, `DELETE FROM membership_entries`, `DELETE FROM groups`} {
		if _, err := h.st.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.api.backfillGroup(context.Background()); err != nil {
		t.Fatal(err)
	}
	l, _ := h.groupLog()
	if !l.State().Active(a.ID) {
		t.Fatal("an existing machine must be written to a new log")
	}
	if h.entryCount() == 0 {
		t.Fatal("nothing written")
	}
}

func TestTheMemberListTableMatchesTheLog(t *testing.T) {
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	a := h.addNamed("Office PC")
	b := h.addNamed("Studio Mac")
	h.mustCall("DELETE", "/api/devices/"+b.ID, nil, nil, 200)
	rows, err := h.st.DB().Query(`SELECT member_id, status, overlay_address, name FROM members ORDER BY member_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string][3]string{}
	for rows.Next() {
		var id, status, name string
		var addr sql.NullString
		rows.Scan(&id, &status, &addr, &name)
		seen[id] = [3]string{status, addr.String, name}
	}
	if seen[a.ID][0] != "active" || seen[a.ID][1] == "" || seen[a.ID][2] != "Office PC" {
		t.Fatalf("%v", seen[a.ID])
	}
	if seen[b.ID][0] != "removed" {
		t.Fatalf("%v", seen[b.ID])
	}
}

func TestEverythingElseBehavesAsBefore(t *testing.T) {
	// The Phase 1 and 2 suites in this package are the real proof (they run unchanged); this checks the
	// one visible promise: adding a machine still answers the same and the log never blocks a request.
	h := newHarness(t)
	h.connectedServer(t.TempDir())
	var out struct {
		Device machine `json:"device"`
	}
	h.mustCall("POST", "/api/devices", map[string]any{"platform": "ios", "name": "Maria's phone"}, &out, 201)
	if out.Device.Name != "Maria's phone" {
		t.Fatalf("%+v", out.Device)
	}
}
