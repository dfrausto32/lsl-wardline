package membership

import "sort"

// Peer is a member another member should have a network connection with, as the log describes it.
type Peer struct {
	MemberID string
	WGPub    string
	Relay    bool
	Gateway  bool
	Voter    bool
}

// Peers is the peer set the log implies: every active member that has a network key, in a fixed order
// (by member id) so that applying it twice changes nothing.
func (s *State) Peers() []Peer {
	var out []Peer
	for _, m := range s.ActiveMembers() {
		if m.WGPub == "" {
			continue
		}
		out = append(out, Peer{MemberID: m.ID, WGPub: m.WGPub, Relay: m.Roles.Relay, Gateway: m.Roles.Gateway, Voter: m.Roles.Voter})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MemberID < out[j].MemberID })
	return out
}

// Depth is how far an entry is from the first one (0 for the first entry itself).
func (l *Log) Depth(id string) (int, bool) {
	n, ok := l.nodes[id]
	if !ok || n.res.Status != StatusApplied {
		return 0, false
	}
	return n.depth, true
}
