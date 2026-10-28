// Package ipam picks a private address range for a network and hands out device
// addresses. People never see or type an address range (specs/BUSINESS-RULES.md).
package ipam

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"net/netip"
	"time"
)

// QuarantineDuration is how long a removed device's address is held back, so a
// stale config on an old phone cannot collide with a new device.
const QuarantineDuration = 7 * 24 * time.Hour

var (
	ErrNoRange = errors.New("no free private address range is available")
	ErrFull    = errors.New("the network has no free addresses left")
)

// Q is satisfied by *sql.DB and *sql.Tx.
type Q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Intner is the part of *rand.Rand that ChooseRange needs.
type Intner interface{ Intn(n int) int }

// DefaultRand returns a randomly seeded source for production use.
func DefaultRand() Intner { return rand.New(rand.NewSource(time.Now().UnixNano())) }

// CommonRanges are ranges that homes, offices, VPN products and container
// tools already use. A new network never picks one of these.
func CommonRanges() []netip.Prefix {
	var out []netip.Prefix
	for _, second := range []int{0, 1, 2, 8, 10, 20, 30, 42, 88, 89, 96, 99, 254} {
		out = append(out, netip.MustParsePrefix(fmt.Sprintf("10.%d.0.0/16", second)))
	}
	out = append(out, netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16"))
	return out
}

// ChooseRange picks a random private /24 inside 10.0.0.0/8 that overlaps
// neither the common ranges nor anything in avoid (for example the ranges
// already present on the host).
func ChooseRange(avoid []netip.Prefix, r Intner) (netip.Prefix, error) {
	bad := append(CommonRanges(), avoid...)
	var candidates []netip.Prefix
	for x := 0; x < 256; x++ {
		for y := 0; y < 256; y++ {
			p := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(x), byte(y), 0}), 24)
			ok := true
			for _, b := range bad {
				if b.Overlaps(p) {
					ok = false
					break
				}
			}
			if ok {
				candidates = append(candidates, p)
			}
		}
	}
	if len(candidates) == 0 {
		return netip.Prefix{}, ErrNoRange
	}
	return candidates[r.Intn(len(candidates))], nil
}

// Allocator hands out device addresses stored in ip_allocations.
type Allocator struct {
	DB  *sql.DB
	Now func() time.Time
}

func (a *Allocator) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

// usable returns the host addresses of p excluding the network and broadcast
// addresses. The first of them belongs to the server.
func usable(p netip.Prefix) []netip.Addr {
	p = p.Masked()
	var out []netip.Addr
	for ip := p.Addr().Next(); p.Contains(ip); ip = ip.Next() {
		if !p.Contains(ip.Next()) { // last address is the broadcast address
			break
		}
		out = append(out, ip)
	}
	return out
}

func networkPrefix(ctx context.Context, q Q, networkID string) (netip.Prefix, error) {
	var s string
	if err := q.QueryRowContext(ctx, `SELECT address_range FROM networks WHERE id=?`, networkID).Scan(&s); err != nil {
		return netip.Prefix{}, err
	}
	return netip.ParsePrefix(s)
}

// Init reserves the server's address (the first usable one) for a new network.
func (a *Allocator) Init(ctx context.Context, q Q, networkID string, p netip.Prefix) error {
	hosts := usable(p)
	if len(hosts) < 2 {
		return fmt.Errorf("range %v is too small", p)
	}
	_, err := q.ExecContext(ctx, `INSERT INTO ip_allocations(network_id, ip, device_id, state) VALUES(?,?,NULL,'reserved')
		ON CONFLICT(network_id, ip) DO NOTHING`, networkID, hosts[0].String())
	return err
}

// ServerIP returns the server's tunnel address.
func (a *Allocator) ServerIP(ctx context.Context, q Q, networkID string) (netip.Addr, error) {
	p, err := networkPrefix(ctx, q, networkID)
	if err != nil {
		return netip.Addr{}, err
	}
	return usable(p)[0], nil
}

type allocRow struct {
	state    string
	released sql.NullString
}

// Allocate gives the device the lowest free address. Calling it again for the
// same device returns the same address.
func (a *Allocator) Allocate(ctx context.Context, q Q, networkID, deviceID string) (netip.Addr, error) {
	p, err := networkPrefix(ctx, q, networkID)
	if err != nil {
		return netip.Addr{}, err
	}
	var mine string
	err = q.QueryRowContext(ctx, `SELECT ip FROM ip_allocations WHERE network_id=? AND device_id=? AND state='allocated'`, networkID, deviceID).Scan(&mine)
	if err == nil {
		return netip.ParseAddr(mine)
	}
	if err != sql.ErrNoRows {
		return netip.Addr{}, err
	}

	rows, err := q.QueryContext(ctx, `SELECT ip, state, released_at FROM ip_allocations WHERE network_id=?`, networkID)
	if err != nil {
		return netip.Addr{}, err
	}
	taken := map[string]allocRow{}
	for rows.Next() {
		var ip string
		var r allocRow
		if err := rows.Scan(&ip, &r.state, &r.released); err != nil {
			rows.Close()
			return netip.Addr{}, err
		}
		taken[ip] = r
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return netip.Addr{}, err
	}
	rows.Close()

	now := a.now()
	for _, ip := range usable(p) {
		if r, ok := taken[ip.String()]; ok {
			if r.state != "quarantined" {
				continue
			}
			rel, perr := time.Parse(time.RFC3339, r.released.String)
			if perr != nil || now.Before(rel.Add(QuarantineDuration)) {
				continue
			}
		}
		_, err := q.ExecContext(ctx, `INSERT INTO ip_allocations(network_id, ip, device_id, state, released_at) VALUES(?,?,?,'allocated',NULL)
			ON CONFLICT(network_id, ip) DO UPDATE SET device_id=excluded.device_id, state='allocated', released_at=NULL`,
			networkID, ip.String(), deviceID)
		if err != nil {
			return netip.Addr{}, err
		}
		return ip, nil
	}
	return netip.Addr{}, ErrFull
}

// Release puts the device's address into quarantine.
func (a *Allocator) Release(ctx context.Context, q Q, networkID, deviceID string) error {
	res, err := q.ExecContext(ctx, `UPDATE ip_allocations SET state='quarantined', released_at=? WHERE network_id=? AND device_id=? AND state='allocated'`,
		a.now().Format(time.RFC3339), networkID, deviceID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("device %s has no address to release", deviceID)
	}
	return nil
}
