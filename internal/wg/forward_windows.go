//go:build windows

package wg

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// platformForwarding uses Windows' own per-interface switch. It is off by default, which is what
// keeps devices isolated, so turning it on is the only change that opens anything.
func platformRelay(ctx context.Context, iface string, p RelayPolicy) error {
	if err := checkIface(iface); err != nil {
		return err
	}
	if !p.All && len(p.ReachAll) > 0 {
		return errors.New("letting only some machines reach the others is not supported on a Windows server yet")
	}
	enabled := p.All
	if out, err := exec.CommandContext(ctx, "netsh", netshForwardingArgs(iface, enabled)...).CombinedOutput(); err != nil {
		return fmt.Errorf("this computer would not change relaying between devices: %w: %s", err, out)
	}
	return nil
}
