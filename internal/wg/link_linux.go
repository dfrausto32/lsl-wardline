//go:build linux

package wg

import (
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
)

// setAddress assigns the address to the interface and brings it up.
func setAddress(name string, p netip.Prefix) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}
	ip := net.IP(p.Addr().AsSlice())
	mask := net.CIDRMask(p.Bits(), p.Addr().BitLen())
	if err := netlink.AddrReplace(link, &netlink.Addr{IPNet: &net.IPNet{IP: ip, Mask: mask}}); err != nil {
		return err
	}
	return netlink.LinkSetUp(link)
}
