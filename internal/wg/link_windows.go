//go:build windows

package wg

import (
	"fmt"
	"net/netip"
	"os/exec"
	"time"

	"golang.zx2c4.com/wireguard/device"
)

// setAddress gives the Wintun adapter its address. The adapter can take a
// moment to show up for netsh after it is created, so each step is retried.
func setAddress(name string, p netip.Prefix) error {
	if !p.Addr().Is4() {
		return fmt.Errorf("only IPv4 addresses are supported on Windows (got %s)", p)
	}
	for _, args := range netshAddressCommands(name, p, device.DefaultMTU) {
		var out []byte
		var err error
		for i := 0; i < 10; i++ {
			if out, err = exec.Command("netsh", args...).CombinedOutput(); err == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if err != nil {
			return fmt.Errorf("netsh %v: %w: %s", args, err, out)
		}
	}
	return nil
}
