// Package portmap asks a home or office router to send a port to this computer, using UPnP, the
// protocol most routers offer for exactly that. It is optional: many routers have it turned off,
// and then the person is shown the manual steps instead.
package portmap

import (
	"context"
	"net/netip"
)

// Gateway is one router service that can open ports. goupnp's clients satisfy it.
type Gateway interface {
	AddPortMapping(remoteHost string, externalPort uint16, protocol string, internalPort uint16, internalClient string, enabled bool, description string, leaseSeconds uint32) error
	GetExternalIPAddress() (string, error)
	DeletePortMapping(remoteHost string, externalPort uint16, protocol string) error
}

type Reason string

const (
	NoRouter Reason = "no_router" // nothing answered, so the router has it off or there is none
	Refused  Reason = "refused"   // a router answered but would not open the port
)

// Result is what happened. RouterAddress is the address the router itself has on the internet; when
// that is not a public one, the internet provider shares one address between customers (Shared).
type Result struct {
	Opened        bool
	Reason        Reason
	RouterAddress string
	Shared        bool
	NeedsRenewal  bool // the router only keeps the rule for a time, so it must be asked again
}

const description = "Wardline"

// A router that does not accept "forever" usually accepts a day, which is then renewed.
const dayLease = 24 * 60 * 60

// Open asks each router service in turn to send the UDP port to internalIP.
func Open(_ context.Context, gws []Gateway, port uint16, internalIP string) Result {
	if len(gws) == 0 {
		return Result{Reason: NoRouter}
	}
	for _, g := range gws {
		renew := false
		err := g.AddPortMapping("", port, "UDP", port, internalIP, true, description, 0)
		if err != nil {
			renew = true
			err = g.AddPortMapping("", port, "UDP", port, internalIP, true, description, dayLease)
		}
		if err != nil {
			continue
		}
		r := Result{Opened: true, NeedsRenewal: renew}
		if ext, err := g.GetExternalIPAddress(); err == nil {
			r.RouterAddress = ext
			if a, err := netip.ParseAddr(ext); err == nil {
				r.Shared = !IsPublic(a)
			}
		}
		return r
	}
	return Result{Reason: Refused}
}

// IsPublic reports whether an address is on the public internet (not private, loopback, link-local
// or the shared range some internet providers use).
func IsPublic(ip netip.Addr) bool {
	shared := netip.MustParsePrefix("100.64.0.0/10")
	return !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() || shared.Contains(ip))
}
