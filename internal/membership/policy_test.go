package membership

import "testing"

// Who may add and remove members (specs/MEMBERSHIP-AND-MODES.md section 2).

func TestInAManagedGroupOnlyTheAuthorityAddsAndRemoves(t *testing.T) {
	boss, sam, kim, lee := newActor(t, "boss"), newActor(t, "sam"), newActor(t, "kim"), newActor(t, "lee")
	w := newWorld(t, "authority", boss)
	w.mustDo(boss, TypeJoin, joinBody(sam, boss))
	w.mustDo(boss, TypeJoin, joinBody(kim, boss))

	_, r := w.do(sam, TypeJoin, joinBody(lee, sam))
	wantRejected(t, r, "authority")
	_, r = w.do(sam, TypeRemoveMember, removeBody(kim, false))
	wantRejected(t, r, "authority")
	_, r = w.do(sam, TypeSetRole, roleBody(sam, "admin"))
	wantRejected(t, r, "authority")

	w.mustDo(boss, TypeRemoveMember, removeBody(kim, false))
	if w.log.State().Active(kim.id) {
		t.Fatal("kim should be gone")
	}
	_, r = w.do(boss, TypeRemoveMember, removeBody(boss, false))
	wantRejected(t, r, "authority cannot be removed")
}

func TestAMemberChangesOnlyItsOwnKey(t *testing.T) {
	boss, sam, kim := newActor(t, "boss"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "authority", boss)
	w.mustDo(boss, TypeJoin, joinBody(sam, boss))
	w.mustDo(boss, TypeJoin, joinBody(kim, boss))
	newKey := b64(make([]byte, 32))
	w.mustDo(sam, TypeMemberKeyRotate, map[string]any{"subject": sam.id, "wg_pub": newKey})
	if got := w.log.State().Members[sam.id].WGPub; got != newKey {
		t.Fatalf("key not changed: %s", got)
	}
	_, r := w.do(sam, TypeMemberKeyRotate, map[string]any{"subject": kim.id, "wg_pub": newKey})
	wantRejected(t, r, "own key")
}

func TestTheAuthorityCanAddComputersThatHaveNoSigningKeyYet(t *testing.T) {
	boss := newActor(t, "boss")
	w := newWorld(t, "authority", boss)
	dev := map[string]any{"member": map[string]any{"id": "dev-01ABC", "name": "Office PC", "wg_pub": b64(make([]byte, 32))}}
	w.mustDo(boss, TypeJoin, dev)
	m := w.log.State().Members["dev-01ABC"]
	if m == nil || m.SigningPub != "" || m.Removed {
		t.Fatalf("%+v", m)
	}
	// Such a computer cannot write entries (it has no key to sign with), and the authority rotates its key for it.
	w.mustDo(boss, TypeMemberKeyRotate, map[string]any{"subject": "dev-01ABC", "wg_pub": b64(make([]byte, 32))})
	// A member id must match its signing key when it has one.
	liar := newActor(t, "liar")
	bad := liar.member()
	bad["id"] = "not-the-hash-of-the-key"
	_, r := w.do(boss, TypeJoin, map[string]any{"member": bad})
	wantRejected(t, r, "does not match")
}

func TestInAnOpenGroupAnyMemberAddsAndRemovesAnyMember(t *testing.T) {
	founder, sam, kim, lee := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim"), newActor(t, "lee")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(sam, TypeJoin, joinBody(kim, sam))                 // sam, not the founder, brings kim in
	w.mustDo(kim, TypeJoin, joinBody(lee, kim))                 // and kim brings lee in
	w.mustDo(lee, TypeRemoveMember, removeBody(founder, false)) // anyone may remove anyone, the founder included (Dylan's decision)
	st := w.log.State()
	if st.Active(founder.id) || !st.Active(lee.id) || !st.Active(sam.id) {
		t.Fatalf("wrong members: %+v", st.Members)
	}
	// Someone who is not in the group cannot add anyone.
	stranger := newActor(t, "stranger")
	_, r := w.do(stranger, TypeJoin, joinBody(newActor(t, "x"), stranger))
	wantRejected(t, r, "not a member")
	// The inviter named must be a member too.
	_, r = w.do(sam, TypeJoin, joinBody(newActor(t, "y"), stranger))
	wantRejected(t, r, "inviter")
	// A member may remove itself.
	w.mustDo(lee, TypeRemoveMember, removeBody(lee, false))
}

func TestInAnOpenGroupOnlyAdminsChangeRoles(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(founder, TypeJoin, joinBody(kim, founder))
	_, r := w.do(sam, TypeSetRole, roleBody(kim, "admin"))
	wantRejected(t, r, "admin")
	w.mustDo(founder, TypeSetRole, roleBody(sam, "admin"))
	w.mustDo(sam, TypeSetRole, roleBody(kim, "admin")) // sam is an admin now
	if !w.log.State().Members[kim.id].Admin {
		t.Fatal("kim should be an admin")
	}
	// A member may set its own node roles; others may not (unless admin).
	w.mustDo(kim, TypeSetNodeRoles, nodeRolesBody(kim, true, false, false))
	if !w.log.State().Members[kim.id].Roles.Relay {
		t.Fatal("relay not set")
	}
}

func TestAGroupKindThatIsNotBuiltIsRefusedClearly(t *testing.T) {
	boss := newActor(t, "boss")
	g := genesisEntry(t, boss, "admins_k_of_n")
	l := NewLog(g.ID)
	wantRejected(t, l.Add(g), "not built")
	g2 := genesisEntry(t, boss, "something-else")
	wantRejected(t, NewLog(g2.ID).Add(g2), "kind of group")
}

func TestAnEntryWrittenAfterTheAuthorWasRemovedIsRefusedButOneWrittenAtTheSameTimeStands(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(founder, TypeJoin, joinBody(kim, founder))
	base := w.log.Heads()

	// kim writes an entry at the same time as sam removes kim: neither has seen the other.
	concurrent := w.entry(kim, TypeSetNodeRoles, nodeRolesBody(kim, true, false, false), base...)
	removal := w.entry(sam, TypeRemoveMember, removeBody(kim, false), base...)
	if r := w.log.Add(concurrent); r.Status != StatusApplied {
		t.Fatalf("%s %s", r.Status, r.Reason)
	}
	if r := w.log.Add(removal); r.Status != StatusApplied {
		t.Fatalf("%s %s", r.Status, r.Reason)
	}
	// Once kim has seen the removal, kim cannot write on top of it.
	after := w.entry(kim, TypeSetNodeRoles, nodeRolesBody(kim, false, false, false), removal.ID, concurrent.ID)
	wantRejected(t, w.log.Add(after), "removed")
	st := w.log.State()
	if st.Active(kim.id) {
		t.Fatal("kim must be out")
	}
	if !st.Members[kim.id].Roles.Relay {
		t.Fatal("what kim wrote while still a member stays on record")
	}
}

func TestSigningKeysAreCheckedAgainstTheMemberWhoWroteTheEntry(t *testing.T) {
	boss, sam, mallory := newActor(t, "boss"), newActor(t, "sam"), newActor(t, "mallory")
	w := newWorld(t, "authority", boss)
	w.mustDo(boss, TypeJoin, joinBody(sam, boss))
	// mallory signs an entry but claims to be the authority.
	e := &Entry{GroupID: w.group, Author: boss.id, Parents: w.log.Heads(), Type: TypeRemoveMember, Body: removeBody(sam, false), ObservedAt: "2026-11-02T11:00:00Z"}
	if err := e.Sign(mallory.signer()); err != nil {
		t.Fatal(err)
	}
	wantRejected(t, w.log.Add(e), "signature")
	if !w.log.State().Active(sam.id) {
		t.Fatal("a forged removal must change nothing")
	}
}
