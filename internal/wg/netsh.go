package wg

import (
	"fmt"
	"net"
	"net/netip"
)

// netshAddressCommands returns the netsh arguments that give a Windows
// interface its address (which also adds the route for its network) and MTU.
// It lives outside the Windows file so the commands can be tested anywhere.
func netshAddressCommands(name string, p netip.Prefix, mtu int) [][]string {
	mask := net.IP(net.CIDRMask(p.Bits(), p.Addr().BitLen())).String()
	return [][]string{
		{"interface", "ipv4", "set", "address", "name=" + name, "source=static", "address=" + p.Addr().String(), "mask=" + mask},
		{"interface", "ipv4", "set", "subinterface", "interface=" + name, fmt.Sprintf("mtu=%d", mtu), "store=active"},
	}
}
