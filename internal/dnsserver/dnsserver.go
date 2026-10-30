// Package dnsserver answers name lookups for the machines on a Wardline network, and nothing else. It
// knows one ending (such as wardline.internal), answers A records from a lookup function, refuses every
// other name, never forwards a question anywhere and keeps no record of what was asked.
package dnsserver

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"
)

const (
	TypeA    uint16 = 1
	TypeAAAA uint16 = 28

	RcodeOK             = 0
	RcodeNameError      = 3
	RcodeNotImplemented = 4
	RcodeRefused        = 5

	classIN = 1
	ttl     = 30 // short, so a rename or removal is seen quickly
)

// Handler answers questions about one ending.
type Handler struct {
	Zone   string                                // e.g. wardline.internal
	Lookup func(label string) (netip.Addr, bool) // a one-part name under the ending
}

type question struct {
	name  string
	qtype uint16
	end   int // offset just after the question in the packet
}

// Answer returns the reply to a query packet, or nil if the packet should be ignored.
func (h *Handler) Answer(pkt []byte) []byte {
	if len(pkt) < 12 || pkt[2]&0x80 != 0 { // too short, or already a response
		return nil
	}
	opcode := (pkt[2] >> 3) & 0x0f
	if binary.BigEndian.Uint16(pkt[4:6]) != 1 {
		return reply(pkt, 12, RcodeRefused, nil, false)
	}
	q, ok := readQuestion(pkt)
	if !ok {
		return nil
	}
	if opcode != 0 {
		return reply(pkt, q.end, RcodeNotImplemented, nil, false)
	}
	zone := strings.ToLower(strings.TrimSuffix(h.Zone, "."))
	name := strings.ToLower(strings.TrimSuffix(q.name, "."))
	if name != zone && !strings.HasSuffix(name, "."+zone) {
		return reply(pkt, q.end, RcodeRefused, nil, false)
	}
	label := strings.TrimSuffix(name, "."+zone)
	if name != zone && !strings.Contains(label, ".") {
		if addr, ok := h.Lookup(label); ok && addr.Is4() {
			if q.qtype == TypeA {
				a := addr.As4()
				return reply(pkt, q.end, RcodeOK, a[:], true)
			}
			return reply(pkt, q.end, RcodeOK, nil, true) // the name exists, but not as this kind of record
		}
	}
	return reply(pkt, q.end, RcodeNameError, nil, true)
}

func readQuestion(pkt []byte) (question, bool) {
	var labels []string
	i := 12
	for {
		if i >= len(pkt) {
			return question{}, false
		}
		n := int(pkt[i])
		if n == 0 {
			i++
			break
		}
		if n&0xC0 != 0 || i+1+n > len(pkt) { // compression does not belong in a question
			return question{}, false
		}
		labels = append(labels, string(pkt[i+1:i+1+n]))
		i += 1 + n
	}
	if i+4 > len(pkt) {
		return question{}, false
	}
	return question{name: strings.Join(labels, "."), qtype: binary.BigEndian.Uint16(pkt[i : i+2]), end: i + 4}, true
}

// reply builds a response to the query in pkt, repeating its question (up to qend) and adding one A answer if ip is set.
func reply(pkt []byte, qend, rcode int, ip []byte, authoritative bool) []byte {
	out := make([]byte, 0, qend+16)
	out = append(out, pkt[:2]...)                                    // same id
	flags := uint16(0x8000) | uint16(pkt[2]&0x01)<<8 | uint16(rcode) // a response; copies "recursion desired"
	if authoritative {
		flags |= 0x0400
	}
	out = binary.BigEndian.AppendUint16(out, flags)
	qd, an := uint16(1), uint16(0)
	if qend == 12 {
		qd = 0
	}
	if ip != nil {
		an = 1
	}
	out = binary.BigEndian.AppendUint16(out, qd)
	out = binary.BigEndian.AppendUint16(out, an)
	out = append(out, 0, 0, 0, 0) // no authority or extra records
	if qend > 12 {
		out = append(out, pkt[12:qend]...)
	}
	if ip != nil {
		out = append(out, 0xC0, 0x0C) // the name, pointing back at the question
		out = binary.BigEndian.AppendUint16(out, TypeA)
		out = binary.BigEndian.AppendUint16(out, classIN)
		out = binary.BigEndian.AppendUint32(out, ttl)
		out = binary.BigEndian.AppendUint16(out, uint16(len(ip)))
		out = append(out, ip...)
	}
	return out
}

// Serve answers queries arriving on pc until ctx ends. It closes pc.
func Serve(ctx context.Context, pc net.PacketConn, h *Handler) {
	go func() { <-ctx.Done(); pc.Close() }()
	buf := make([]byte, 1500)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return
		}
		if resp := h.Answer(buf[:n]); resp != nil {
			pc.WriteTo(resp, from)
		}
	}
}

// ---- a small client, for tests and for "wardline names lookup" ----

// BuildQuery makes a query packet for one name.
func BuildQuery(id uint16, name string, qtype uint16) []byte {
	out := binary.BigEndian.AppendUint16(nil, id)
	out = binary.BigEndian.AppendUint16(out, 0x0100) // recursion desired
	out = append(out, 0, 1, 0, 0, 0, 0, 0, 0)
	for _, l := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		out = append(out, byte(len(l)))
		out = append(out, l...)
	}
	out = append(out, 0)
	out = binary.BigEndian.AppendUint16(out, qtype)
	return binary.BigEndian.AppendUint16(out, classIN)
}

type Response struct {
	ID            uint16
	Response      bool
	Authoritative bool
	Rcode         int
	Addrs         []netip.Addr
}

// ParseResponse reads the parts of a reply that matter here.
func ParseResponse(pkt []byte) (Response, error) {
	if len(pkt) < 12 {
		return Response{}, errors.New("reply too short")
	}
	flags := binary.BigEndian.Uint16(pkt[2:4])
	r := Response{ID: binary.BigEndian.Uint16(pkt[0:2]), Response: flags&0x8000 != 0, Authoritative: flags&0x0400 != 0, Rcode: int(flags & 0x0f)}
	qd, an := int(binary.BigEndian.Uint16(pkt[4:6])), int(binary.BigEndian.Uint16(pkt[6:8]))
	i := 12
	skipName := func() bool {
		for i < len(pkt) {
			n := int(pkt[i])
			switch {
			case n == 0:
				i++
				return true
			case n&0xC0 == 0xC0:
				i += 2
				return true
			default:
				i += 1 + n
			}
		}
		return false
	}
	for k := 0; k < qd; k++ {
		if !skipName() {
			return r, errors.New("bad reply")
		}
		i += 4
	}
	for k := 0; k < an; k++ {
		if !skipName() || i+10 > len(pkt) {
			return r, errors.New("bad reply")
		}
		typ, rdlen := binary.BigEndian.Uint16(pkt[i:i+2]), int(binary.BigEndian.Uint16(pkt[i+8:i+10]))
		i += 10
		if i+rdlen > len(pkt) {
			return r, errors.New("bad reply")
		}
		if typ == TypeA && rdlen == 4 {
			r.Addrs = append(r.Addrs, netip.AddrFrom4([4]byte(pkt[i:i+4])))
		}
		i += rdlen
	}
	return r, nil
}

// Query asks one server (host:port) for the address of a name.
func Query(ctx context.Context, server, name string) ([]netip.Addr, int, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", server)
	if err != nil {
		return nil, 0, err
	}
	defer c.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(3 * time.Second)
	}
	c.SetDeadline(deadline)
	if _, err := c.Write(BuildQuery(uint16(time.Now().UnixNano()), name, TypeA)); err != nil {
		return nil, 0, err
	}
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil {
		return nil, 0, err
	}
	r, err := ParseResponse(buf[:n])
	return r.Addrs, r.Rcode, err
}
