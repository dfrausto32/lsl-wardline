package membership

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"lsl-wardline/internal/names"
)

type Status string

const (
	StatusApplied  Status = "applied"
	StatusQueued   Status = "queued"   // waiting for a parent that has not arrived
	StatusRejected Status = "rejected" // never applied and never passed on as valid
)

// Result says what became of an entry, and why in plain words if it was not applied.
type Result struct {
	Status Status
	Reason string
}

// Log holds one group's entries and works out who is in. The same entries give the same state on every
// node, in whatever order they arrived. Whether an entry is valid depends only on its ancestors, so a result,
// once known, never changes.
type Log struct {
	groupID string
	nodes   map[string]*node
	state   *State
	dirty   bool
}

// NewLog starts an empty log for the group whose genesis entry has the given id.
func NewLog(groupID string) *Log {
	return &Log{groupID: groupID, nodes: map[string]*node{}, dirty: true}
}

func sortedCopy(ids []string) []string {
	out := append([]string{}, ids...)
	sort.Strings(out)
	return out
}

func (l *Log) GroupID() string { return l.groupID }

// Add puts an entry in the log and says what became of it. Adding the same entry again changes nothing.
func (l *Log) Add(e *Entry) Result {
	if e == nil {
		return Result{StatusRejected, "there is no entry"}
	}
	if err := e.check(); err != nil {
		return Result{StatusRejected, err.Error()}
	}
	// The id is the hash of the contents. An entry claiming another id is turned away without being stored,
	// so a forged copy can never take the place of the real one.
	if e.computeID() != e.ID {
		return Result{StatusRejected, ErrBadID.Error()}
	}
	if cur, ok := l.nodes[e.ID]; ok {
		if cur.res.Status != StatusQueued {
			return cur.res
		}
		cur.e = e // a waiting entry is replaced by the newest copy, in case the first was a forgery
	} else {
		l.nodes[e.ID] = &node{e: e}
	}
	n := l.nodes[e.ID]
	l.resolve(n)
	if n.res.Status == StatusRejected && n.dropWhenRejected {
		delete(l.nodes, e.ID)
		return n.res
	}
	l.resolveWaiting()
	l.dirty = true
	return n.res
}

// Result returns what became of an entry the log knows.
func (l *Log) Result(id string) (Result, bool) {
	n, ok := l.nodes[id]
	if !ok {
		return Result{}, false
	}
	return n.res, true
}

// Entries returns every entry the log holds, applied or not, ordered by id.
func (l *Log) Entries() []*Entry {
	out := make([]*Entry, 0, len(l.nodes))
	for _, n := range l.nodes {
		out = append(out, n.e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Heads are the applied entries that no other applied entry lists as a parent.
func (l *Log) Heads() []string {
	child := map[string]bool{}
	for _, n := range l.nodes {
		if n.res.Status == StatusApplied {
			for _, p := range n.e.Parents {
				child[p] = true
			}
		}
	}
	out := []string{}
	for id, n := range l.nodes {
		if n.res.Status == StatusApplied && !child[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// State is who is in the group, from every applied entry.
func (l *Log) State() *State {
	if l.state != nil && !l.dirty {
		return l.state
	}
	var applied []*node
	for _, n := range l.nodes {
		if n.res.Status == StatusApplied {
			applied = append(applied, n)
		}
	}
	l.state, l.dirty = fold(l.groupID, applied), false
	return l.state
}

// resolveWaiting gives every waiting entry another try, until nothing more changes.
func (l *Log) resolveWaiting() {
	for {
		changed := false
		ids := make([]string, 0)
		for id, n := range l.nodes {
			if n.res.Status == StatusQueued {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			n := l.nodes[id]
			before := n.res
			l.resolve(n)
			if n.res.Status == StatusRejected && n.dropWhenRejected {
				delete(l.nodes, id)
				changed = true
				continue
			}
			if n.res != before {
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (l *Log) reject(n *node, reason string) {
	n.res = Result{StatusRejected, reason}
}

// resolve works out whether an entry is valid, from its ancestors alone.
func (l *Log) resolve(n *node) {
	e := n.e
	n.dropWhenRejected = false
	if e.Type == TypeGenesis {
		if reason, drop := l.validateGenesis(e); reason != "" {
			l.reject(n, reason)
			n.dropWhenRejected = drop
			return
		}
		n.depth, n.res = 0, Result{Status: StatusApplied}
		return
	}
	if e.GroupID != l.groupID {
		l.reject(n, "the entry belongs to a different group")
		return
	}
	if len(e.Parents) == 0 {
		l.reject(n, "an entry other than the first must say which entries it follows")
		return
	}
	depth := 0
	for _, p := range e.Parents {
		pn := l.nodes[p]
		switch {
		case pn == nil:
			n.res = Result{StatusQueued, fmt.Sprintf("waiting for the entry %s that it follows", short(p))}
			return
		case pn.res.Status == StatusRejected:
			l.reject(n, fmt.Sprintf("an entry it follows (%s) was refused", short(p)))
			return
		case pn.res.Status == StatusQueued:
			n.res = Result{StatusQueued, fmt.Sprintf("waiting for the entry %s that it follows, which is itself waiting", short(p))}
			return
		}
		if pn.depth > depth {
			depth = pn.depth
		}
	}
	n.depth = depth + 1
	past := fold(l.groupID, l.ancestors(e))
	reason, drop := l.validate(e, past)
	if reason != "" {
		l.reject(n, reason)
		n.dropWhenRejected = drop
		return
	}
	n.res = Result{Status: StatusApplied}
}

// ancestors are all the entries before e, which are all applied (that is checked before this is called).
func (l *Log) ancestors(e *Entry) []*node {
	seen := map[string]bool{}
	var out []*node
	stack := append([]string{}, e.Parents...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		n := l.nodes[id]
		out = append(out, n)
		stack = append(stack, n.e.Parents...)
	}
	return out
}

// ---- checking one entry ----

var (
	memberIDRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	b64Key     = func(s string) bool { b, err := base64.StdEncoding.DecodeString(s); return err == nil && len(b) == 32 }
)

func str(body map[string]any, key string) (string, bool) { s, ok := body[key].(string); return s, ok }

func (l *Log) validateGenesis(e *Entry) (reason string, drop bool) {
	if e.ID != l.groupID {
		return "the entry belongs to a different group", false
	}
	if len(e.Parents) != 0 || e.GroupID != "" {
		return "the first entry of a group cannot follow anything", false
	}
	pol, _ := str(e.Body, "policy")
	if _, err := PolicyFor(Mode(pol)); err != nil {
		return err.Error(), false
	}
	if name, ok := str(e.Body, "name"); !ok || name == "" || len(name) > 100 {
		return "a group needs a name of up to 100 characters", false
	}
	f, ok := e.Body["founder"].(map[string]any)
	if !ok {
		return "the first entry must say who founded the group", false
	}
	if reason := checkMember(f, true); reason != "" {
		return reason, false
	}
	if id, _ := str(f, "id"); id != e.Author {
		return "the first entry must be written by the founder", false
	}
	pub, _ := base64.StdEncoding.DecodeString(f["signing_pub"].(string))
	if err := e.Verify(ed25519.PublicKey(pub)); err != nil {
		return err.Error(), errors.Is(err, ErrBadSignature)
	}
	return "", false
}

// checkMember checks a member description. needKey says a signing key is required.
func checkMember(m map[string]any, needKey bool) string {
	id, _ := str(m, "id")
	if !memberIDRE.MatchString(id) {
		return "the member has no usable id"
	}
	if name, _ := str(m, "name"); len(name) > 100 {
		return "a member's name can be up to 100 characters"
	}
	if h, ok := str(m, "hostname_label"); ok && h != "" {
		if err := names.Check(h); err != nil {
			return "the member's name on the network can't be used: " + err.Error()
		}
	}
	if w, ok := str(m, "wg_pub"); ok && w != "" && !b64Key(w) {
		return "the member's network key is not valid"
	}
	sp, ok := str(m, "signing_pub")
	if !ok || sp == "" {
		if needKey {
			return "the member needs a signing key"
		}
		return ""
	}
	if !b64Key(sp) {
		return "the member's signing key is not valid"
	}
	raw, _ := base64.StdEncoding.DecodeString(sp)
	if MemberID(ed25519.PublicKey(raw)) != id {
		return "the member id does not match its signing key"
	}
	return ""
}

// validate checks a non-genesis entry against the group as it stood before it. drop is true when the entry
// failed because its signature or id is wrong, in which case it is not kept (so a forged copy cannot block the real one).
func (l *Log) validate(e *Entry, past *State) (reason string, drop bool) {
	author := past.Members[e.Author]
	switch {
	case author == nil:
		return "the author was not a member of the group", false
	case author.Removed:
		return "the author had been removed from the group", false
	case author.SigningPub == "":
		return "the author has no signing key, so it cannot write entries", false
	}
	raw, _ := base64.StdEncoding.DecodeString(author.SigningPub)
	if err := e.Verify(ed25519.PublicKey(raw)); err != nil {
		return err.Error(), true
	}
	pol, err := PolicyFor(past.Mode)
	if err != nil {
		return err.Error(), false
	}
	subject, _ := str(e.Body, "subject")
	active := func(id string) bool { return past.Active(id) }

	switch e.Type {
	case TypeGenesis:
		return "a group has only one first entry", false
	case TypeJoin:
		m, ok := e.Body["member"].(map[string]any)
		if !ok {
			return "a join must describe the member", false
		}
		if reason := checkMember(m, past.Mode != ModeAuthority); reason != "" {
			return reason, false
		}
		id, _ := str(m, "id")
		if cur := past.Members[id]; cur != nil {
			if cur.Removed {
				return "that member was removed and cannot come back with the same identity", false
			}
			return "that member is already in the group", false
		}
		inviter, ok := str(e.Body, "inviter")
		if !ok || inviter == "" {
			inviter = e.Author
		}
		if err := pol.CanJoin(past, e.Author, inviter); err != nil {
			return err.Error(), false
		}
	case TypeRemoveMember:
		if _, ok := e.Body["cascade"].(bool); !ok {
			return "a removal must say whether it also removes the people they brought in", false
		}
		if !active(subject) {
			return "that member is not in the group", false
		}
		if err := pol.CanRemove(past, e.Author, subject); err != nil {
			return err.Error(), false
		}
	case TypeSetRole:
		if role, _ := str(e.Body, "role"); role != "admin" && role != "member" {
			return "a role is either admin or member", false
		}
		if !active(subject) {
			return "that member is not in the group", false
		}
		if err := pol.CanSetRole(past, e.Author, subject); err != nil {
			return err.Error(), false
		}
	case TypeSetNodeRoles:
		for _, k := range []string{"relay", "gateway", "voter"} {
			if _, ok := e.Body[k].(bool); !ok {
				return "what a member does must say yes or no for relay, gateway and voter", false
			}
		}
		if !active(subject) {
			return "that member is not in the group", false
		}
		if err := pol.CanSetNodeRoles(past, e.Author, subject); err != nil {
			return err.Error(), false
		}
	case TypeMemberKeyRotate:
		if w, _ := str(e.Body, "wg_pub"); !b64Key(w) {
			return "the new network key is not valid", false
		}
		if !active(subject) {
			return "that member is not in the group", false
		}
		if err := pol.CanRotateKey(past, e.Author, subject); err != nil {
			return err.Error(), false
		}
	case TypeRevokeInvite:
		if id, _ := str(e.Body, "invite_id"); id == "" {
			return "an invite must be named", false
		}
		if err := pol.CanRevokeInvite(past, e.Author); err != nil {
			return err.Error(), false
		}
	}
	return "", false
}
