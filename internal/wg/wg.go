// Package wg is the only place Wardline touches WireGuard. Everything else
// talks to the Backend interface, which has three implementations: an
// in-memory fake (unit tests), a userspace one (wireguard-go, works in any
// container and on any OS) and a Linux kernel one (build-tagged).
package wg

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"time"
)

// InterfaceConfig describes the local WireGuard interface.
type InterfaceConfig struct {
	PrivateKey string // base64; for a server this is the server's own key
	ListenPort int    // 0 lets the system choose
	Address    netip.Prefix
}

// PeerConfig is the desired state of one peer. Keys are base64.
type PeerConfig struct {
	PublicKey    string
	PresharedKey string // "" for none
	AllowedIPs   []netip.Prefix
	Keepalive    int    // seconds, 0 = off
	Endpoint     string // "" = roaming (the usual server-side case)
}

// PeerState is a peer as read back from the live interface.
type PeerState struct {
	PeerConfig
	LastHandshake time.Time // zero if the peer never connected
	RxBytes       int64
	TxBytes       int64
	LastEndpoint  string // where the peer was last seen
}

// Diff is the difference between desired and live peers.
type Diff struct {
	Add    []PeerConfig
	Update []PeerConfig
	Remove []string // public keys
}

func (d Diff) IsEmpty() bool { return len(d.Add) == 0 && len(d.Update) == 0 && len(d.Remove) == 0 }

// Backend applies WireGuard state. All implementations satisfy the same
// contract test suite (contract_test.go).
type Backend interface {
	// Configure creates the interface if it does not exist and sets its key,
	// port and address. Calling it again with the same name is allowed.
	Configure(ctx context.Context, name string, cfg InterfaceConfig) error
	// Peers returns the live peers, sorted by public key.
	Peers(ctx context.Context, name string) ([]PeerState, error)
	// ApplyDiff changes only what the diff names.
	ApplyDiff(ctx context.Context, name string, d Diff) error
	// Remove deletes the interface. Removing one that is gone is not an error.
	Remove(ctx context.Context, name string) error
}

func sortedIPs(in []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, len(in))
	for i, p := range in {
		out[i] = p.Masked()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func sameIPs(a, b []netip.Prefix) bool {
	a, b = sortedIPs(a), sortedIPs(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func samePeer(want, have PeerConfig) bool {
	if want.PresharedKey != have.PresharedKey || want.Keepalive != have.Keepalive {
		return false
	}
	// The server never configures an endpoint (devices roam), so the live one is ignored then.
	if want.Endpoint != "" && want.Endpoint != have.Endpoint {
		return false
	}
	return sameIPs(want.AllowedIPs, have.AllowedIPs)
}

// ComputeDiff returns what must change to make live match desired. Output is
// sorted by public key so it is stable.
func ComputeDiff(desired []PeerConfig, live []PeerState) Diff {
	have := make(map[string]PeerState, len(live))
	for _, p := range live {
		have[p.PublicKey] = p
	}
	want := make(map[string]bool, len(desired))
	var d Diff
	for _, p := range desired {
		want[p.PublicKey] = true
		cur, ok := have[p.PublicKey]
		switch {
		case !ok:
			d.Add = append(d.Add, p)
		case !samePeer(p, cur.PeerConfig):
			d.Update = append(d.Update, p)
		}
	}
	for k := range have {
		if !want[k] {
			d.Remove = append(d.Remove, k)
		}
	}
	sort.Slice(d.Add, func(i, j int) bool { return d.Add[i].PublicKey < d.Add[j].PublicKey })
	sort.Slice(d.Update, func(i, j int) bool { return d.Update[i].PublicKey < d.Update[j].PublicKey })
	sort.Strings(d.Remove)
	return d
}

// Reconcile reads the live state, computes the difference and applies only
// that. Running it twice with the same input changes nothing the second time.
func Reconcile(ctx context.Context, b Backend, name string, desired []PeerConfig) (Diff, error) {
	seen := map[string]bool{}
	for _, p := range desired {
		if err := ValidateKey(p.PublicKey); err != nil {
			return Diff{}, fmt.Errorf("peer has an invalid key: %w", err)
		}
		if seen[p.PublicKey] {
			return Diff{}, fmt.Errorf("two peers share the public key %s", p.PublicKey)
		}
		seen[p.PublicKey] = true
	}
	live, err := b.Peers(ctx, name)
	if err != nil {
		return Diff{}, err
	}
	d := ComputeDiff(desired, live)
	if d.IsEmpty() {
		return d, nil
	}
	return d, b.ApplyDiff(ctx, name, d)
}
