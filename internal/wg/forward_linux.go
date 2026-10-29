//go:build linux

package wg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// platformForwarding makes the host match the setting.
//
// The firewall rules are what enforce it: they block relaying completely when off and allow only
// device-to-device relaying when on. The host's own per-interface switch is also set, which relaying
// needs on a host that does not already relay. Containers usually cannot change that switch, so when
// it cannot be written it is only an error if relaying is wanted and the switch is not already on.
func platformForwarding(ctx context.Context, iface string, enabled bool) error {
	path, err := forwardingPath(iface)
	if err != nil {
		return err
	}
	rules, err := nftRules(iface, enabled)
	if err != nil {
		return err
	}
	rulesErr := loadRules(ctx, rules)
	if rulesErr != nil && enabled {
		return fmt.Errorf("could not set up the safety rules for relaying between devices, so it stays off (install nftables on this computer): %w", rulesErr)
	}

	value := "0"
	if enabled {
		value = "1"
	}
	werr := os.WriteFile(path, []byte(value+"\n"), 0o644)
	if werr != nil {
		if cur, rerr := os.ReadFile(path); rerr == nil && strings.TrimSpace(string(cur)) == value {
			werr = nil // already the way it should be
		}
	}
	switch {
	case enabled && werr != nil:
		return fmt.Errorf("this computer would not turn on relaying between devices: %w", werr)
	case !enabled && rulesErr != nil && werr != nil:
		return fmt.Errorf("could not keep devices apart: no firewall rules (%v) and the switch could not be turned off (%v)", rulesErr, werr)
	}
	return nil
}

func loadRules(ctx context.Context, rules string) error {
	nft, err := exec.LookPath("nft")
	if err != nil {
		return errors.New("the nft program was not found")
	}
	cmd := exec.CommandContext(ctx, nft, "-f", "-")
	cmd.Stdin = bytes.NewBufferString(rules)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft: %w: %s", err, out)
	}
	return nil
}
