package gateway

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"sync"

	"lsl-wardline/internal/dnsserver"
)

// The name service answers the machines' names on the server's own network address, from the records in
// the instructions it gets. It runs only while the instructions carry names (devices can reach each other).

type nameService struct {
	mu      sync.Mutex
	zone    string
	records map[string]netip.Addr
	addr    netip.Addr
	cancel  context.CancelFunc
	done    chan struct{}
}

func (n *nameService) lookup(label string) (netip.Addr, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	a, ok := n.records[label]
	return a, ok
}

func listenNames(a netip.Addr) (net.PacketConn, error) {
	return net.ListenPacket("udp", net.JoinHostPort(a.String(), "53"))
}

// applyNames starts, updates or stops the name service to match the instructions. A failure is logged and
// retried with the next instructions; it never stops the rest of the server from being set up.
func (g *Gateway) applyNames(st DesiredState, own netip.Addr) {
	if st.NamesDomain == "" {
		g.stopNames()
		return
	}
	recs := make(map[string]netip.Addr, len(st.Names))
	for _, r := range st.Names {
		if a, err := netip.ParseAddr(r.IP); err == nil {
			recs[strings.ToLower(r.Name)] = a
		}
	}
	g.nsMu.Lock()
	defer g.nsMu.Unlock()
	if g.ns != nil && g.ns.addr == own && g.ns.zone == st.NamesDomain {
		g.ns.mu.Lock()
		g.ns.records = recs
		g.ns.mu.Unlock()
		return
	}
	g.stopNamesLocked()
	listen := g.cfg.ListenNames
	if listen == nil {
		listen = listenNames
	}
	pc, err := listen(own)
	if err != nil {
		g.cfg.Log.Warn("the name service could not start, so machines cannot find each other by name yet", "address", own.String(), "error", err.Error())
		return
	}
	ns := &nameService{zone: st.NamesDomain, records: recs, addr: own, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	ns.cancel = cancel
	go func() {
		defer close(ns.done)
		dnsserver.Serve(ctx, pc, &dnsserver.Handler{Zone: ns.zone, Lookup: ns.lookup})
	}()
	g.ns = ns
	g.cfg.Log.Info("the name service is running", "address", own.String(), "names", len(recs))
}

func (g *Gateway) stopNames() {
	g.nsMu.Lock()
	defer g.nsMu.Unlock()
	g.stopNamesLocked()
}

func (g *Gateway) stopNamesLocked() {
	if g.ns == nil {
		return
	}
	g.ns.cancel()
	<-g.ns.done
	g.ns = nil
}
