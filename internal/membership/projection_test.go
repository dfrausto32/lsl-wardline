package membership

import (
	"testing"
)

func TestThePeerSetIsTheActiveMembersThatHaveANetworkKey(t *testing.T) {
	founder, sam, kim, lee := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim"), newActor(t, "lee")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(founder, TypeJoin, joinBody(kim, founder))
	w.mustDo(founder, TypeJoin, joinBody(lee, founder))
	w.mustDo(founder, TypeRemoveMember, removeBody(kim, false))
	w.mustDo(sam, TypeSetNodeRoles, nodeRolesBody(sam, true, false, false))

	peers := w.log.State().Peers()
	ids := map[string]Peer{}
	for _, p := range peers {
		ids[p.MemberID] = p
	}
	if _, ok := ids[kim.id]; ok {
		t.Fatal("a removed member must not be a peer")
	}
	if len(peers) != 3 {
		t.Fatalf("want founder, sam and lee; got %d", len(peers))
	}
	if !ids[sam.id].Relay || ids[lee.id].Relay {
		t.Fatalf("roles not carried: %+v", ids)
	}
	for i := 1; i < len(peers); i++ {
		if peers[i-1].MemberID >= peers[i].MemberID {
			t.Fatal("peers must come in a fixed order so applying them twice changes nothing")
		}
	}
}

func TestAMemberWithoutANetworkKeyIsNotAPeerYet(t *testing.T) {
	boss := newActor(t, "boss")
	w := newWorld(t, "authority", boss)
	w.mustDo(boss, TypeJoin, map[string]any{"member": map[string]any{"id": "dev-1", "name": "Waiting PC"}})
	for _, p := range w.log.State().Peers() {
		if p.MemberID == "dev-1" {
			t.Fatal("a computer that has not made its key yet cannot be a peer")
		}
	}
	if !w.log.State().Active("dev-1") {
		t.Fatal("but it is a member")
	}
}

func TestDepthIsReportedForStorage(t *testing.T) {
	founder, sam := newActor(t, "founder"), newActor(t, "sam")
	w := newWorld(t, "authority", founder)
	e := w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	if d, ok := w.log.Depth(e.ID); !ok || d != 1 {
		t.Fatalf("depth %d %v", d, ok)
	}
	if d, _ := w.log.Depth(w.group); d != 0 {
		t.Fatalf("the first entry has depth 0, got %d", d)
	}
}
