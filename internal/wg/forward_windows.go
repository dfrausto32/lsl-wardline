//go:build windows

package wg

import (
	"context"
	"fmt"
	"os/exec"
)

// platformForwarding uses Windows' own per-interface switch. It is off by default, which is what
// keeps devices isolated, so turning it on is the only change that opens anything.
func platformForwarding(ctx context.Context, iface string, enabled bool) error {
	if err := checkIface(iface); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "netsh", netshForwardingArgs(iface, enabled)...).CombinedOutput(); err != nil {
		return fmt.Errorf("this computer would not change relaying between devices: %w: %s", err, out)
	}
	return nil
}
