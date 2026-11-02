package membership

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// Every node that has the same entries reaches the same state, in any order of arrival
// (specs/MEMBERSHIP-AND-MODES.md section 4.3).

func TestRemoveWinsOverAConcurrentRoleChange(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(founder, TypeJoin, joinBody(kim, founder))
	base := w.log.Heads()
	removal := w.entry(sam, TypeRemoveMember, removeBody(kim, false), base...)
	promote := w.entry(founder, TypeSetRole, roleBody(kim, "admin"), base...)
	for _, order := range [][]*Entry{{removal, promote}, {promote, removal}} {
		l := replay(t, w, order)
		st := l.State()
		if st.Active(kim.id) {
			t.Fatalf("a removal must win whichever order they arrive in")
		}
	}
}

func TestRemovingSomeoneWithCascadeRemovesWhomTheyBroughtIn(t *testing.T) {
	founder, sam, kim, lee, pat := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim"), newActor(t, "lee"), newActor(t, "pat")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(sam, TypeJoin, joinBody(kim, sam))
	w.mustDo(kim, TypeJoin, joinBody(lee, kim))
	w.mustDo(founder, TypeJoin, joinBody(pat, founder))
	w.mustDo(founder, TypeRemoveMember, removeBody(sam, true))
	st := w.log.State()
	for _, gone := range []actor{sam, kim, lee} {
		if st.Active(gone.id) {
			t.Fatalf("%s should have been removed with sam", gone.name)
		}
	}
	if !st.Active(pat.id) || !st.Active(founder.id) {
		t.Fatal("people sam did not bring in must stay")
	}
}

func TestRemovingSomeoneWithoutCascadeKeepsWhomTheyBroughtIn(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(sam, TypeJoin, joinBody(kim, sam))
	w.mustDo(founder, TypeRemoveMember, removeBody(sam, false))
	st := w.log.State()
	if st.Active(sam.id) || !st.Active(kim.id) {
		t.Fatalf("only sam should be gone: %+v", st.Members)
	}
}

func TestCascadeReachesPeopleBroughtInAtTheSameTimeAsTheRemoval(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	base := w.log.Heads()
	// Sam brings kim in while the founder, who has not seen it, removes sam and everyone sam added.
	join := w.entry(sam, TypeJoin, joinBody(kim, sam), base...)
	removal := w.entry(founder, TypeRemoveMember, removeBody(sam, true), base...)
	for _, order := range [][]*Entry{{join, removal}, {removal, join}} {
		st := replay(t, w, order).State()
		if st.Active(kim.id) || st.Active(sam.id) {
			t.Fatalf("kim was brought in by sam, so the cascade must reach kim in either order (%v)", st.Members[kim.id])
		}
	}
}

func TestOfTwoConcurrentRoleChangesTheOneWithTheGreaterDepthThenIdWins(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(founder, TypeJoin, joinBody(kim, founder))
	w.mustDo(founder, TypeSetRole, roleBody(sam, "admin"))
	base := w.log.Heads()
	// One side makes kim an admin straight away. The other side first changes something else, then removes kim's admin role:
	// that branch is one entry deeper, so its change wins.
	promote := w.entry(founder, TypeSetRole, roleBody(kim, "admin"), base...)
	step := w.entry(sam, TypeSetNodeRoles, nodeRolesBody(sam, true, false, false), base...)
	demote := w.entry(sam, TypeSetRole, roleBody(kim, "member"), step.ID)
	for _, order := range [][]*Entry{{promote, step, demote}, {demote, step, promote}, {step, promote, demote}} {
		st := replay(t, w, order).State()
		if st.Members[kim.id].Admin {
			t.Fatalf("the deeper change (member) should win in every order")
		}
	}
}

func TestWithEqualDepthTheHigherIdWinsInEveryOrder(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	w := newWorld(t, "open", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(founder, TypeJoin, joinBody(kim, founder))
	w.mustDo(founder, TypeSetRole, roleBody(sam, "admin"))
	base := w.log.Heads()
	a := w.entry(founder, TypeSetRole, roleBody(kim, "admin"), base...)
	b := w.entry(sam, TypeSetRole, roleBody(kim, "member"), base...)
	wantAdmin := a.ID > b.ID
	for _, order := range [][]*Entry{{a, b}, {b, a}} {
		if got := replay(t, w, order).State().Members[kim.id].Admin; got != wantAdmin {
			t.Fatalf("admin=%v, want %v (the entry with the higher id wins)", got, wantAdmin)
		}
	}
}

func TestAnEntryThatArrivesBeforeItsParentWaitsAndThenApplies(t *testing.T) {
	founder, sam, kim := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "kim")
	src := newWorld(t, "open", founder)
	first := src.mustDo(founder, TypeJoin, joinBody(sam, founder))
	second := src.mustDo(sam, TypeJoin, joinBody(kim, sam))

	l := NewLog(src.group)
	l.Add(src.genesis())
	r := l.Add(second)
	if r.Status != StatusQueued || !containsFold(r.Reason, "waiting") {
		t.Fatalf("an entry without its parent must wait, with a reason: %s %q", r.Status, r.Reason)
	}
	if l.State().Active(kim.id) {
		t.Fatal("a waiting entry must not change anything")
	}
	if got, _ := l.Result(second.ID); got.Status != StatusQueued {
		t.Fatal("still waiting")
	}
	l.Add(first)
	if got, _ := l.Result(second.ID); got.Status != StatusApplied {
		t.Fatalf("the child should apply once its parent arrives: %s %q", got.Status, got.Reason)
	}
	if !l.State().Active(kim.id) {
		t.Fatal("kim should be in now")
	}
}

func TestAnEntryWhoseParentWasRefusedIsRefusedToo(t *testing.T) {
	founder, sam, stranger := newActor(t, "founder"), newActor(t, "sam"), newActor(t, "stranger")
	w := newWorld(t, "authority", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	bad := w.entry(stranger, TypeJoin, joinBody(newActor(t, "x"), stranger))
	wantRejected(t, w.log.Add(bad), "not a member")
	child := w.entry(founder, TypeSetRole, roleBody(sam, "admin"), bad.ID)
	wantRejected(t, w.log.Add(child), "refused")
}

func TestTamperedForgedAndForeignEntriesAreRefusedWithAReason(t *testing.T) {
	founder, sam := newActor(t, "founder"), newActor(t, "sam")
	w := newWorld(t, "authority", founder)
	e := w.entry(founder, TypeJoin, joinBody(sam, founder))
	e.Body["inviter"] = "someone-else" // changed after signing
	wantRejected(t, w.log.Add(e), "contents")

	other := newWorld(t, "authority", newActor(t, "other-founder"))
	foreign := other.entry(founder, TypeJoin, joinBody(sam, founder))
	wantRejected(t, w.log.Add(foreign), "different group")

	// Adding the same good entry twice changes nothing.
	good := w.entry(founder, TypeJoin, joinBody(sam, founder))
	if r := w.log.Add(good); r.Status != StatusApplied {
		t.Fatal(r.Reason)
	}
	h := w.log.State().Hash()
	if r := w.log.Add(good); r.Status != StatusApplied || w.log.State().Hash() != h {
		t.Fatal("a repeat must change nothing")
	}
}

func TestTheSameJoinTwiceDoesNotBringARemovedMemberBack(t *testing.T) {
	founder, sam := newActor(t, "founder"), newActor(t, "sam")
	w := newWorld(t, "authority", founder)
	w.mustDo(founder, TypeJoin, joinBody(sam, founder))
	w.mustDo(founder, TypeRemoveMember, removeBody(sam, false))
	_, r := w.do(founder, TypeJoin, joinBody(sam, founder))
	wantRejected(t, r, "removed")
	if w.log.State().Active(sam.id) {
		t.Fatal("removal is permanent for that member id; a machine comes back only with a fresh join and new keys")
	}
}

// ---- convergence under random concurrency ----

// replay builds a fresh log from everything already in the world's log (the shared history), then the given
// entries in the given order.
func replay(t *testing.T, w *world, entries []*Entry) *Log {
	t.Helper()
	l := NewLog(w.group)
	for _, e := range w.log.Entries() {
		l.Add(e)
	}
	for _, e := range entries {
		l.Add(e)
	}
	return l
}

func (w *world) genesis() *Entry {
	for _, e := range w.log.Entries() {
		if e.Type == TypeGenesis {
			return e
		}
	}
	w.t.Fatal("no genesis")
	return nil
}

// replica is one node during the simulation: its own log, which it extends and merges with others.
type replica struct {
	log *Log
}

func TestEveryNodeReachesTheSameStateWhateverTheOrder(t *testing.T) {
	for seed := int64(1); seed <= 12; seed++ {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			convergenceRound(t, seed, []string{"open", "authority"}[seed%2])
		})
	}
}

func convergenceRound(t *testing.T, seed int64, policy string) {
	rng := rand.New(rand.NewSource(seed))
	founder := newActor(t, "founder")
	people := []actor{founder}
	for i := 0; i < 9; i++ {
		people = append(people, newActor(t, fmt.Sprintf("p%d", i)))
	}
	w := newWorld(t, policy, founder)
	gen := w.genesis()
	reps := make([]*replica, 3)
	for i := range reps {
		l := NewLog(w.group)
		l.Add(gen)
		reps[i] = &replica{log: l}
	}
	next := 1 // the next person who has not joined yet
	var accepted []*Entry
	seen := map[string]bool{gen.ID: true}

	sync := func(a, b *replica) { // a sends everything it has to b
		for _, e := range a.log.Entries() {
			if r, _ := a.log.Result(e.ID); r.Status == StatusApplied {
				b.log.Add(e)
			}
		}
	}

	for step := 0; step < 70; step++ {
		r := reps[rng.Intn(len(reps))]
		st := r.log.State()
		var active []actor
		for _, p := range people {
			if st.Active(p.id) {
				active = append(active, p)
			}
		}
		if len(active) == 0 {
			continue
		}
		author := active[rng.Intn(len(active))]
		if policy == "authority" {
			author = founder // only the authority writes in a managed group
		}
		var typ Type
		var body map[string]any
		switch k := rng.Intn(10); {
		case k < 4 && next < len(people):
			joiner := people[next]
			typ, body = TypeJoin, joinBody(joiner, author)
			next++
		case k < 6:
			// Remove someone other than the founder, mostly without cascade, so the group does not die out early.
			target := people[1+rng.Intn(len(people)-1)]
			typ, body = TypeRemoveMember, removeBody(target, rng.Intn(4) == 0)
		case k < 8:
			target := people[rng.Intn(len(people))]
			typ, body = TypeSetRole, roleBody(target, []string{"admin", "member"}[rng.Intn(2)])
		default:
			target := people[rng.Intn(len(people))]
			typ, body = TypeSetNodeRoles, nodeRolesBody(target, rng.Intn(2) == 0, rng.Intn(2) == 0, rng.Intn(2) == 0)
		}
		w.tick++
		e := &Entry{GroupID: w.group, Author: author.id, Parents: sortedCopy(r.log.Heads()), Type: typ, Body: body,
			ObservedAt: fmt.Sprintf("2026-11-02T10:%02d:00Z", w.tick%60)}
		if err := e.Sign(author.signer()); err != nil {
			t.Fatal(err)
		}
		if res := r.log.Add(e); res.Status == StatusApplied && !seen[e.ID] {
			seen[e.ID] = true
			accepted = append(accepted, e)
		}
		if rng.Intn(4) == 0 { // sometimes two replicas catch up with each other, and so stop being concurrent
			a, b := reps[rng.Intn(len(reps))], reps[rng.Intn(len(reps))]
			sync(a, b)
		}
	}
	if len(accepted) < 10 {
		t.Fatalf("the simulation made only %d entries", len(accepted))
	}

	// Entries valid for the replica that wrote them are valid for everyone (validity depends only on ancestors).
	want := ""
	for round := 0; round < 25; round++ {
		order := append([]*Entry{}, accepted...)
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		if round%5 == 0 { // duplicates and a delivery of everything twice
			order = append(order, order...)
		}
		l := replay(t, w, order)
		for _, e := range accepted {
			if res, _ := l.Result(e.ID); res.Status != StatusApplied {
				t.Fatalf("round %d: %s entry %s was %s (%s) although its writer accepted it", round, e.Type, e.ID[:8], res.Status, res.Reason)
			}
		}
		h := l.State().Hash()
		if want == "" {
			want = h
		} else if h != want {
			t.Fatalf("round %d: the state differs from the first order", round)
		}
	}
	// And a node that syncs with everyone ends in the same place as the shuffled ones.
	for _, r := range reps {
		for _, o := range reps {
			sync(o, r)
		}
	}
	if got := reps[0].log.State().Hash(); !strings.EqualFold(got, want) {
		t.Fatalf("a replica that merged everyone's entries disagrees with the shuffled replays")
	}
}
