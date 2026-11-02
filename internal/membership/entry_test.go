package membership

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestCanonicalIsTheSameWhateverTheKeyOrder(t *testing.T) {
	a := map[string]any{"b": int64(2), "a": []any{"x", true, int64(-7)}, "c": map[string]any{"z": "1", "y": "2"}}
	b := map[string]any{"c": map[string]any{"y": "2", "z": "1"}, "a": []any{"x", true, int64(-7)}, "b": 2}
	ca, err := Canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := Canonical(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ca) != string(cb) {
		t.Fatalf("%s != %s", ca, cb)
	}
	if want := `{"a":["x",true,-7],"b":2,"c":{"y":"2","z":"1"}}`; string(ca) != want {
		t.Fatalf("got %s want %s", ca, want)
	}
}

func TestCanonicalWritesStringsTheDocumentedWay(t *testing.T) {
	got, err := Canonical(map[string]any{"s": "a<b>&\"q\"\\\né\u0001"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"s\":\"a<b>&\\\"q\\\"\\\\\\né\\u0001\"}"; string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestCanonicalRefusesWhatItCannotSpellOneWay(t *testing.T) {
	for name, v := range map[string]any{
		"a float":          map[string]any{"x": 1.5},
		"a nil":            map[string]any{"x": nil},
		"invalid UTF-8":    map[string]any{"x": "\xff\xfe"},
		"an unknown type":  map[string]any{"x": struct{}{}},
		"a non-map at top": []any{int64(1)},
	} {
		if _, err := Canonical(v); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func sampleJoin(t *testing.T, author ed25519.PrivateKey, group string) *Entry {
	t.Helper()
	e := &Entry{
		GroupID: group, Author: MemberID(author.Public().(ed25519.PublicKey)), Parents: []string{strings.Repeat("a", 64)},
		Type: TypeJoin, ObservedAt: "2026-11-02T10:00:00Z",
		Body: map[string]any{"member": map[string]any{"id": "dev1", "name": "Office PC", "hostname_label": "office-pc"}, "inviter": "x"},
	}
	if err := e.Sign(KeySigner{author}); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAnEntryIsSignedAndItsIdIsTheHashOfWhatWasSigned(t *testing.T) {
	k := newKey(t)
	e := sampleJoin(t, k, strings.Repeat("b", 64))
	signed, err := e.SignedBytes()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(signed)
	if e.ID != hex.EncodeToString(sum[:]) {
		t.Fatalf("id %s is not the hash of the signed bytes", e.ID)
	}
	if err := e.Verify(k.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	// The signed bytes never include the id or the signature, so they can be computed before either exists.
	if strings.Contains(string(signed), e.ID) || strings.Contains(string(signed), `"sig"`) {
		t.Fatalf("signed bytes contain the id or signature: %s", signed)
	}
}

func TestATamperedEntryIsRefusedWithAReason(t *testing.T) {
	k := newKey(t)
	pub := k.Public().(ed25519.PublicKey)
	for name, tamper := range map[string]func(*Entry){
		"a changed body":   func(e *Entry) { e.Body["inviter"] = "someone-else" },
		"a changed author": func(e *Entry) { e.Author = strings.Repeat("c", 64) },
		"a changed parent": func(e *Entry) { e.Parents = []string{strings.Repeat("d", 64)} },
		"a changed time":   func(e *Entry) { e.ObservedAt = "2030-01-01T00:00:00Z" },
		"a changed type":   func(e *Entry) { e.Type = TypeRemoveMember },
		"a changed group":  func(e *Entry) { e.GroupID = strings.Repeat("e", 64) },
	} {
		e := sampleJoin(t, k, strings.Repeat("b", 64))
		tamper(e)
		if err := e.Verify(pub); !errors.Is(err, ErrBadID) {
			t.Errorf("%s: got %v, want ErrBadID", name, err)
		}
		// Even if the attacker recomputes the id to match, the signature no longer fits.
		e.ID = e.computeID()
		if err := e.Verify(pub); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s with a fixed id: got %v, want ErrBadSignature", name, err)
		}
	}
	e := sampleJoin(t, k, strings.Repeat("b", 64))
	e.Sig[0] ^= 1
	if err := e.Verify(pub); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a changed signature: %v", err)
	}
	other := newKey(t).Public().(ed25519.PublicKey)
	if err := sampleJoin(t, k, strings.Repeat("b", 64)).Verify(other); !errors.Is(err, ErrBadSignature) {
		t.Errorf("another key: %v", err)
	}
}

func TestParentsMustBeSortedAndUnique(t *testing.T) {
	k := newKey(t)
	e := sampleJoin(t, k, strings.Repeat("b", 64))
	e.Parents = []string{strings.Repeat("b", 64), strings.Repeat("a", 64)}
	if err := e.Sign(KeySigner{k}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("unsorted parents: %v", err)
	}
	e.Parents = []string{strings.Repeat("a", 64), strings.Repeat("a", 64)}
	if err := e.Sign(KeySigner{k}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("repeated parents: %v", err)
	}
}

func TestAnEntryTravelsAsTextAndStillVerifies(t *testing.T) {
	k := newKey(t)
	e := sampleJoin(t, k, strings.Repeat("b", 64))
	wire, err := e.Wire()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseWire(wire)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != e.ID || string(back.Sig) != string(e.Sig) {
		t.Fatalf("round trip changed it: %+v", back)
	}
	if err := back.Verify(k.Public().(ed25519.PublicKey)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{``, `{}`, `not json`, `{"id":1}`, strings.Replace(string(wire), `"type":"join"`, `"type":"nonsense"`, 1)} {
		if _, err := ParseWire([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// A float anywhere is refused: there is one way to spell a number.
	if _, err := ParseWire([]byte(strings.Replace(string(wire), `"inviter":"x"`, `"inviter":"x","n":1.5`, 1))); err == nil {
		t.Error("accepted a float")
	}
}

func TestAMemberIdIsTheHashOfItsSigningKey(t *testing.T) {
	pub := newKey(t).Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	if MemberID(pub) != hex.EncodeToString(sum[:]) {
		t.Fatal("wrong member id")
	}
}

func TestTheSignerKeepsItsKeyToItself(t *testing.T) {
	k := newKey(t)
	var s Signer = KeySigner{k}
	if !s.Public().Equal(k.Public()) {
		t.Fatal("wrong public key")
	}
	sig, err := s.Sign([]byte("hi"))
	if err != nil || !ed25519.Verify(s.Public(), []byte("hi"), sig) {
		t.Fatalf("signature does not verify: %v", err)
	}
}
