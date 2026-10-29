package wg

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Forwarder is a Backend that can also decide whether the server relays traffic from one device to
// another. Off means devices are isolated from each other, which is the default.
type Forwarder interface {
	SetForwarding(ctx context.Context, iface string, enabled bool) error
}

// Interface names go into file paths and firewall rules, so they are checked first.
var ifaceRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,15}$`)

func checkIface(name string) error {
	if !ifaceRe.MatchString(name) {
		return fmt.Errorf("%q is not a usable network interface name", name)
	}
	return nil
}

// forwardingPath is the Linux switch that decides whether packets arriving on an interface may be relayed.
func forwardingPath(iface string) (string, error) {
	if err := checkIface(iface); err != nil {
		return "", err
	}
	return "/proc/sys/net/ipv4/conf/" + iface + "/forwarding", nil
}

const nftTable = "wardline_forward"

// nftRules returns the firewall rules to load. They are what keeps devices apart, because a host may
// already relay traffic for its own reasons (one running containers, for example), so the host's own
// switch cannot be trusted to mean "off".
//
// Off: nothing is relayed to or from the tunnel at all.
// On: traffic may go from the tunnel back into the tunnel (device to device) and nowhere else, so
// nothing is relayed to or from the rest of the server's network, such as the internet or a cloud
// provider's inside network.
//
// "table" before "delete table" makes the replacement work whether or not the table existed.
func nftRules(iface string, enabled bool) (string, error) {
	if err := checkIface(iface); err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "table inet %s\ndelete table inet %s\n", nftTable, nftTable)
	fmt.Fprintf(&sb, "table inet %s {\n\tchain forward {\n\t\ttype filter hook forward priority filter; policy accept;\n", nftTable)
	if enabled {
		fmt.Fprintf(&sb, "\t\tiifname %q oifname != %q drop\n", iface, iface)
		fmt.Fprintf(&sb, "\t\toifname %q iifname != %q drop\n", iface, iface)
	} else {
		fmt.Fprintf(&sb, "\t\tiifname %q drop\n", iface)
		fmt.Fprintf(&sb, "\t\toifname %q drop\n", iface)
	}
	sb.WriteString("\t}\n}\n")
	return sb.String(), nil
}

// netshForwardingArgs is the Windows switch for relaying packets that arrive on an interface.
func netshForwardingArgs(iface string, enabled bool) []string {
	v := "disabled"
	if enabled {
		v = "enabled"
	}
	return []string{"interface", "ipv4", "set", "interface", "interface=" + iface, "forwarding=" + v}
}

// SetForwarding on the userspace and kernel backends changes the host's own settings.
func (u *Userspace) SetForwarding(ctx context.Context, iface string, enabled bool) error {
	return platformForwarding(ctx, iface, enabled)
}

func (k *Kernel) SetForwarding(ctx context.Context, iface string, enabled bool) error {
	return platformForwarding(ctx, iface, enabled)
}
