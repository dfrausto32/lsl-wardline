package membership

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"
)

// actor is a person with a signing key, standing in for a node.
type actor struct {
	name string
	key  ed25519.PrivateKey
	id   string
}

func newActor(t *testing.T, name string) actor {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return actor{name: name, key: priv, id: MemberID(pub)}
}

func (a actor) signer() Signer { return KeySigner{a.key} }

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func (a actor) member() map[string]any {
	pub := a.key.Public().(ed25519.PublicKey)
	return map[string]any{"id": a.id, "name": a.name, "hostname_label": a.name, "signing_pub": b64(pub), "wg_pub": b64(pub)}
}

func joinBody(joiner, inviter actor) map[string]any {
	return map[string]any{"member": joiner.member(), "inviter": inviter.id}
}

// world is one node's copy of a group's log, with helpers to write entries into it.
type world struct {
	t     *testing.T
	log   *Log
	group string
	tick  int
}

func genesisEntry(t *testing.T, founder actor, policy string) *Entry {
	t.Helper()
	e := &Entry{Author: founder.id, Parents: []string{}, Type: TypeGenesis, ObservedAt: "2026-11-02T09:00:00Z",
		Body: map[string]any{"name": "Test group", "policy": policy, "founder": founder.member()}}
	if err := e.Sign(founder.signer()); err != nil {
		t.Fatal(err)
	}
	return e
}

func newWorld(t *testing.T, policy string, founder actor) *world {
	t.Helper()
	g := genesisEntry(t, founder, policy)
	w := &world{t: t, log: NewLog(g.ID), group: g.ID}
	if r := w.log.Add(g); r.Status != StatusApplied {
		t.Fatalf("genesis was %s: %s", r.Status, r.Reason)
	}
	return w
}

// entry writes and signs an entry whose parents are the given ids (or the log's current heads if none).
func (w *world) entry(author actor, typ Type, body map[string]any, parents ...string) *Entry {
	w.t.Helper()
	if len(parents) == 0 {
		parents = w.log.Heads()
	}
	w.tick++
	e := &Entry{GroupID: w.group, Author: author.id, Parents: sortedCopy(parents), Type: typ, Body: body,
		ObservedAt: fmt.Sprintf("2026-11-02T10:%02d:00Z", w.tick%60)}
	if err := e.Sign(author.signer()); err != nil {
		w.t.Fatal(err)
	}
	return e
}

// do writes an entry on top of the current heads and adds it.
func (w *world) do(author actor, typ Type, body map[string]any) (*Entry, Result) {
	w.t.Helper()
	e := w.entry(author, typ, body)
	return e, w.log.Add(e)
}

func (w *world) mustDo(author actor, typ Type, body map[string]any) *Entry {
	w.t.Helper()
	e, r := w.do(author, typ, body)
	if r.Status != StatusApplied {
		w.t.Fatalf("%s by %s was %s: %s", typ, author.name, r.Status, r.Reason)
	}
	return e
}

func removeBody(subject actor, cascade bool) map[string]any {
	return map[string]any{"subject": subject.id, "cascade": cascade}
}

func roleBody(subject actor, role string) map[string]any {
	return map[string]any{"subject": subject.id, "role": role}
}

func nodeRolesBody(subject actor, relay, gateway, voter bool) map[string]any {
	return map[string]any{"subject": subject.id, "relay": relay, "gateway": gateway, "voter": voter}
}

func wantRejected(t *testing.T, r Result, contains string) {
	t.Helper()
	if r.Status != StatusRejected {
		t.Fatalf("expected a rejection mentioning %q, got %s %q", contains, r.Status, r.Reason)
	}
	if contains != "" && !containsFold(r.Reason, contains) {
		t.Fatalf("reason %q does not mention %q", r.Reason, contains)
	}
}

func containsFold(s, sub string) bool {
	ls, lsub := []rune(s), []rune(sub)
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			if lower(ls[i+j]) != lower(lsub[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
