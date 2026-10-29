package wg

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestNetshCommandsSetAddressMaskAndMTU(t *testing.T) {
	got := netshAddressCommands("wardline0", netip.MustParsePrefix("10.8.0.5/24"), 1420)
	want := [][]string{
		{"interface", "ipv4", "set", "address", "name=wardline0", "source=static", "address=10.8.0.5", "mask=255.255.255.0"},
		{"interface", "ipv4", "set", "subinterface", "interface=wardline0", "mtu=1420", "store=active"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestNetshMaskForOtherSizes(t *testing.T) {
	for p, mask := range map[string]string{"10.0.0.1/8": "255.0.0.0", "192.168.7.9/30": "255.255.255.252", "10.1.2.3/32": "255.255.255.255"} {
		got := netshAddressCommands("x", netip.MustParsePrefix(p), 1420)[0][7]
		if got != "mask="+mask {
			t.Errorf("%s: got %s, want mask=%s", p, got, mask)
		}
	}
}
