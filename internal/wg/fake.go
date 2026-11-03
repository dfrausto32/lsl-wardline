package wg

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"time"
)

// Fake is an in-memory Backend for unit tests. It also lets tests pretend a
// device connected.
type Fake struct {
	mu     sync.Mutex
	ifaces map[string]*fakeIface
	relay  map[string]RelayPolicy
	Now    func() time.Time
}

type fakeIface struct {
	cfg   InterfaceConfig
	peers map[string]*PeerState
}

func NewFake() *Fake { return &Fake{ifaces: map[string]*fakeIface{}, Now: time.Now} }

func (f *Fake) Configure(_ context.Context, name string, cfg InterfaceConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.ifaces[name]; ok {
		i.cfg = cfg
		return nil
	}
	f.ifaces[name] = &fakeIface{cfg: cfg, peers: map[string]*PeerState{}}
	return nil
}

func (f *Fake) get(name string) (*fakeIface, error) {
	i, ok := f.ifaces[name]
	if !ok {
		return nil, fmt.Errorf("interface %s does not exist", name)
	}
	return i, nil
}

func (f *Fake) Peers(_ context.Context, name string) ([]PeerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.get(name)
	if err != nil {
		return nil, err
	}
	out := make([]PeerState, 0, len(i.peers))
	for _, p := range i.peers {
		c := *p
		c.AllowedIPs = sortedIPs(p.AllowedIPs)
		out = append(out, c)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].PublicKey < out[b].PublicKey })
	return out, nil
}

func (f *Fake) ApplyDiff(_ context.Context, name string, d Diff) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, err := f.get(name)
	if err != nil {
		return err
	}
	for _, p := range append(append([]PeerConfig{}, d.Add...), d.Update...) {
		cur, ok := i.peers[p.PublicKey]
		if !ok {
			cur = &PeerState{}
			i.peers[p.PublicKey] = cur
		}
		cur.PeerConfig = p
	}
	for _, k := range d.Remove {
		delete(i.peers, k)
	}
	return nil
}

func (f *Fake) Remove(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.ifaces, name)
	return nil
}

// SimulateHandshake pretends the peer connected just now.
func (f *Fake) SimulateHandshake(name, publicKey string, rx, tx int64, endpoint string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.ifaces[name].peers[publicKey]
	p.LastHandshake = f.Now()
	p.RxBytes, p.TxBytes = rx, tx
	p.LastEndpoint = endpoint
	p.Endpoint = endpoint
}

// SetHandshake lets a test place the last handshake at an exact time.
func (f *Fake) SetHandshake(name, publicKey string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ifaces[name].peers[publicKey].LastHandshake = at
}

// Interface returns the interface settings last applied, so tests can check them.
func (f *Fake) Interface(name string) (InterfaceConfig, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.ifaces[name]
	if !ok {
		return InterfaceConfig{}, false
	}
	return i.cfg, true
}

// SetForwarding records whether the pretend server relays between devices.
func (f *Fake) SetForwarding(ctx context.Context, name string, enabled bool) error {
	return f.SetRelay(ctx, name, RelayPolicy{All: enabled})
}

// SetRelay records who the pretend server relays for. The list is kept sorted, without repeats.
func (f *Fake) SetRelay(_ context.Context, name string, p RelayPolicy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.relay == nil {
		f.relay = map[string]RelayPolicy{}
	}
	seen := map[netip.Addr]bool{}
	var list []netip.Addr
	for _, a := range p.ReachAll {
		if !seen[a] {
			seen[a] = true
			list = append(list, a)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Less(list[j]) })
	f.relay[name] = RelayPolicy{All: p.All, ReachAll: list}
	return nil
}

// Relay reports what SetRelay (or SetForwarding) last set, and whether it was ever set.
func (f *Fake) Relay(name string) (p RelayPolicy, set bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, set = f.relay[name]
	return p, set
}

// Forwarding reports whether the pretend server relays at all, for everybody or for some machines, and whether it was ever set.
func (f *Fake) Forwarding(name string) (on, set bool) {
	p, set := f.Relay(name)
	return p.All || len(p.ReachAll) > 0, set
}
