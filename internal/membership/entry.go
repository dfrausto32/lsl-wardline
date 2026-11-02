// Package membership is the signed, append-only record of who is in a group. Every node that has the same
// entries reaches the same answer about who is in, whatever order the entries arrived in.
// See specs/MEMBERSHIP-AND-MODES.md and docs/MEMBERSHIP-DECISIONS.md.
package membership

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
)

var (
	ErrBadID        = errors.New("the entry's id does not match its contents")
	ErrBadSignature = errors.New("the entry's signature does not match its author")
	ErrMalformed    = errors.New("the entry is not well formed")
)

type Type string

const (
	TypeGenesis         Type = "genesis"
	TypeJoin            Type = "join"
	TypeRemoveMember    Type = "remove_member"
	TypeSetRole         Type = "set_role"
	TypeSetNodeRoles    Type = "set_node_roles"
	TypeMemberKeyRotate Type = "member_key_rotate"
	TypeRevokeInvite    Type = "revoke_invite"
)

var knownTypes = map[Type]bool{TypeGenesis: true, TypeJoin: true, TypeRemoveMember: true, TypeSetRole: true,
	TypeSetNodeRoles: true, TypeMemberKeyRotate: true, TypeRevokeInvite: true}

var hash64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Entry is one line of the log.
type Entry struct {
	ID         string   // hex SHA-256 of the signed bytes
	GroupID    string   // the id of the group's genesis entry; empty on the genesis entry itself
	Author     string   // member id of whoever wrote it
	Parents    []string // ids of the log heads the author had seen, sorted
	Type       Type
	Body       map[string]any // see the type's rules in policy.go
	ObservedAt string         // the author's own clock, for people to read; never used to decide anything
	Sig        []byte
}

// MemberID is the id of a member that has a signing key: the hex SHA-256 of the public key.
func MemberID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

func (e *Entry) check() error {
	if !knownTypes[e.Type] {
		return fmt.Errorf("%w: unknown type %q", ErrMalformed, e.Type)
	}
	if e.Author == "" {
		return fmt.Errorf("%w: no author", ErrMalformed)
	}
	if e.GroupID != "" && !hash64.MatchString(e.GroupID) {
		return fmt.Errorf("%w: bad group id", ErrMalformed)
	}
	if !sort.StringsAreSorted(e.Parents) {
		return fmt.Errorf("%w: parents are not sorted", ErrMalformed)
	}
	for i, p := range e.Parents {
		if !hash64.MatchString(p) {
			return fmt.Errorf("%w: bad parent id", ErrMalformed)
		}
		if i > 0 && p == e.Parents[i-1] {
			return fmt.Errorf("%w: a parent is listed twice", ErrMalformed)
		}
	}
	if e.Body == nil {
		return fmt.Errorf("%w: no body", ErrMalformed)
	}
	return nil
}

func (e *Entry) signedMap() map[string]any {
	m := map[string]any{"author": e.Author, "parents": append([]string{}, e.Parents...), "type": string(e.Type), "body": e.Body, "observed_at": e.ObservedAt}
	if e.GroupID != "" {
		m["group_id"] = e.GroupID
	}
	return m
}

// SignedBytes are the bytes that are hashed to make the id and signed to make the signature.
func (e *Entry) SignedBytes() ([]byte, error) {
	if err := e.check(); err != nil {
		return nil, err
	}
	return Canonical(e.signedMap())
}

func (e *Entry) computeID() string {
	b, err := e.SignedBytes()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Sign fills in the id and the signature.
func (e *Entry) Sign(s Signer) error {
	b, err := e.SignedBytes()
	if err != nil {
		return err
	}
	sig, err := s.Sign(b)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	e.ID, e.Sig = hex.EncodeToString(sum[:]), sig
	return nil
}

// Verify checks the id and the signature against the author's public key.
func (e *Entry) Verify(authorKey ed25519.PublicKey) error {
	b, err := e.SignedBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != e.ID {
		return ErrBadID
	}
	if len(authorKey) != ed25519.PublicKeySize || len(e.Sig) != ed25519.SignatureSize || !ed25519.Verify(authorKey, b, e.Sig) {
		return ErrBadSignature
	}
	return nil
}

// Wire is the entry as text, for storing and sending. It is canonical JSON of the whole entry.
func (e *Entry) Wire() ([]byte, error) {
	m := e.signedMap()
	m["id"] = e.ID
	m["sig"] = base64.StdEncoding.EncodeToString(e.Sig)
	if err := e.check(); err != nil {
		return nil, err
	}
	return Canonical(m)
}

// ParseWire reads an entry from text. It checks the shape, not the signature.
func ParseWire(b []byte) (*Entry, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: extra data after the entry", ErrMalformed)
	}
	norm, err := normalize(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	m := norm.(map[string]any)
	str := func(k string, required bool) (string, error) {
		v, ok := m[k]
		if !ok {
			if required {
				return "", fmt.Errorf("%w: missing %s", ErrMalformed, k)
			}
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("%w: %s is not text", ErrMalformed, k)
		}
		return s, nil
	}
	e := &Entry{}
	var sigText, typ string
	for _, f := range []struct {
		key      string
		dst      *string
		required bool
	}{{"id", &e.ID, true}, {"group_id", &e.GroupID, false}, {"author", &e.Author, true}, {"type", &typ, true}, {"observed_at", &e.ObservedAt, true}, {"sig", &sigText, true}} {
		s, err := str(f.key, f.required)
		if err != nil {
			return nil, err
		}
		*f.dst = s
	}
	e.Type = Type(typ)
	ps, ok := m["parents"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: parents is not a list", ErrMalformed)
	}
	e.Parents = []string{}
	for _, p := range ps {
		s, ok := p.(string)
		if !ok {
			return nil, fmt.Errorf("%w: a parent is not text", ErrMalformed)
		}
		e.Parents = append(e.Parents, s)
	}
	body, ok := m["body"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: body is not an object", ErrMalformed)
	}
	e.Body = body
	if e.Sig, err = base64.StdEncoding.DecodeString(sigText); err != nil {
		return nil, fmt.Errorf("%w: signature is not valid", ErrMalformed)
	}
	if !hash64.MatchString(e.ID) {
		return nil, fmt.Errorf("%w: bad id", ErrMalformed)
	}
	return e, e.check()
}

// normalize turns decoded JSON numbers into int64 and refuses floats.
func normalize(v any) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			n, err := normalize(e)
			if err != nil {
				return nil, err
			}
			x[k] = n
		}
		return x, nil
	case []any:
		for i, e := range x {
			n, err := normalize(e)
			if err != nil {
				return nil, err
			}
			x[i] = n
		}
		return x, nil
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return nil, fmt.Errorf("%s is not a whole number", x)
		}
		return n, nil
	case string, bool:
		return x, nil
	case nil:
		return nil, errors.New("null is not allowed")
	}
	return nil, fmt.Errorf("unexpected %T", v)
}
