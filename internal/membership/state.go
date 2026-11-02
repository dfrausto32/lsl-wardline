package membership

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// Mode is a group's admission policy, fixed when the group is made.
type Mode string

const (
	ModeAuthority Mode = "authority" // shown to people as a Managed group
	ModeOpen      Mode = "open"      // shown as an Open group
	ModeKofN      Mode = "admins_k_of_n"
)

type NodeRoles struct{ Relay, Gateway, Voter bool }

// Member is one node as the log describes it.
type Member struct {
	ID            string
	Name          string
	HostnameLabel string
	SigningPub    string // base64; empty for a computer in a managed group that has no signing key yet
	WGPub         string // base64
	Admin         bool
	Roles         NodeRoles
	AddedBy       string // who brought this member in: the inviter in an open group, the authority in a managed one
	AddedEntry    string
	Removed       bool
	RemovedEntry  string
}

// State is who is in the group, worked out from entries alone.
type State struct {
	GroupID        string
	Name           string
	Mode           Mode
	Authority      string // the founder's member id
	Members        map[string]*Member
	RevokedInvites map[string]bool
}

func (s *State) Active(id string) bool {
	m, ok := s.Members[id]
	return ok && !m.Removed
}

// ActiveMembers lists the members that are still in, ordered by id.
func (s *State) ActiveMembers() []*Member {
	var out []*Member
	for _, m := range s.Members {
		if !m.Removed {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Hash is a fingerprint of the whole state. Two nodes that agree on who is in have the same hash.
func (s *State) Hash() string {
	ids := make([]string, 0, len(s.Members))
	for id := range s.Members {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	members := make([]any, 0, len(ids))
	for _, id := range ids {
		m := s.Members[id]
		members = append(members, map[string]any{
			"id": m.ID, "name": m.Name, "hostname_label": m.HostnameLabel, "signing_pub": m.SigningPub, "wg_pub": m.WGPub,
			"admin": m.Admin, "relay": m.Roles.Relay, "gateway": m.Roles.Gateway, "voter": m.Roles.Voter,
			"added_by": m.AddedBy, "added_entry": m.AddedEntry, "removed": m.Removed, "removed_entry": m.RemovedEntry,
		})
	}
	revoked := make([]string, 0, len(s.RevokedInvites))
	for id := range s.RevokedInvites {
		revoked = append(revoked, id)
	}
	sort.Strings(revoked)
	b, err := Canonical(map[string]any{"group_id": s.GroupID, "name": s.Name, "mode": string(s.Mode), "authority": s.Authority,
		"members": members, "revoked_invites": revoked})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// node is an entry together with what the log has worked out about it.
type node struct {
	e     *Entry
	depth int // 0 for the genesis entry, otherwise one more than the deepest parent
	res   Result
	// dropWhenRejected is set when the entry failed on its signature or id; such an entry is not kept.
	dropWhenRejected bool
}

type rank struct {
	depth int
	id    string
}

func (r rank) beats(o rank) bool { return r.depth > o.depth || (r.depth == o.depth && r.id > o.id) }

// fold works out the state from entries that are all known to be valid. It orders them by (depth, id), which
// puts every parent before its children and breaks ties the same way on every node. Where two entries change
// the same thing at once, the one with the greater (depth, id) wins; a removal always wins.
func fold(groupID string, nodes []*node) *State {
	sorted := append([]*node{}, nodes...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].depth != sorted[j].depth {
			return sorted[i].depth < sorted[j].depth
		}
		return sorted[i].e.ID < sorted[j].e.ID
	})
	st := &State{GroupID: groupID, Members: map[string]*Member{}, RevokedInvites: map[string]bool{}}
	won := map[string]rank{} // "<member>/<field>" -> the entry that last set it
	type cascade struct{ entry, subject string }
	var cascades []cascade

	set := func(subject, field string, n *node) bool {
		k, r := subject+"/"+field, rank{n.depth, n.e.ID}
		if cur, ok := won[k]; ok && !r.beats(cur) {
			return false
		}
		won[k] = r
		return true
	}

	for _, n := range sorted {
		e := n.e
		switch e.Type {
		case TypeGenesis:
			f := e.Body["founder"].(map[string]any)
			st.Name, _ = e.Body["name"].(string)
			st.Mode = Mode(e.Body["policy"].(string))
			m := memberFrom(f)
			m.Admin, m.AddedEntry = true, e.ID
			st.Authority = m.ID
			st.Members[m.ID] = m
		case TypeJoin:
			m := memberFrom(e.Body["member"].(map[string]any))
			if _, exists := st.Members[m.ID]; exists {
				continue // a second join for the same identity changes nothing, and never brings back a removed member
			}
			m.AddedBy, m.AddedEntry = e.Author, e.ID
			if inv, ok := e.Body["inviter"].(string); ok && st.Mode == ModeOpen {
				m.AddedBy = inv
			}
			st.Members[m.ID] = m
		case TypeRemoveMember:
			subject := e.Body["subject"].(string)
			if m := st.Members[subject]; m != nil && !m.Removed {
				m.Removed, m.RemovedEntry = true, e.ID
			}
			if c, _ := e.Body["cascade"].(bool); c {
				cascades = append(cascades, cascade{e.ID, subject})
			}
		case TypeSetRole:
			subject := e.Body["subject"].(string)
			if m := st.Members[subject]; m != nil && set(subject, "role", n) {
				m.Admin = e.Body["role"].(string) == "admin"
			}
		case TypeSetNodeRoles:
			subject := e.Body["subject"].(string)
			if m := st.Members[subject]; m != nil && set(subject, "node", n) {
				m.Roles = NodeRoles{Relay: e.Body["relay"].(bool), Gateway: e.Body["gateway"].(bool), Voter: e.Body["voter"].(bool)}
			}
		case TypeMemberKeyRotate:
			subject := e.Body["subject"].(string)
			if m := st.Members[subject]; m != nil && set(subject, "key", n) {
				m.WGPub = e.Body["wg_pub"].(string)
			}
		case TypeRevokeInvite:
			st.RevokedInvites[e.Body["invite_id"].(string)] = true
		}
	}

	// A cascade removes everyone the subject brought in, over the tree as it stands once every entry is in,
	// so people brought in at the same moment as the removal are included.
	if len(cascades) > 0 {
		children := map[string][]string{}
		for id, m := range st.Members {
			if m.AddedBy != "" && m.AddedBy != id {
				children[m.AddedBy] = append(children[m.AddedBy], id)
			}
		}
		sort.Slice(cascades, func(i, j int) bool { return cascades[i].entry < cascades[j].entry })
		for _, c := range cascades {
			queue := []string{c.subject}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				kids := children[cur]
				sort.Strings(kids)
				for _, k := range kids {
					if m := st.Members[k]; !m.Removed {
						m.Removed, m.RemovedEntry = true, c.entry
					}
					queue = append(queue, k)
				}
			}
		}
	}
	return st
}

func memberFrom(f map[string]any) *Member {
	s := func(k string) string { v, _ := f[k].(string); return v }
	return &Member{ID: s("id"), Name: s("name"), HostnameLabel: s("hostname_label"), SigningPub: s("signing_pub"), WGPub: s("wg_pub")}
}
