//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"lsl-wardline/internal/agent"
)

// linuxNames sets the network's names through systemd-resolved, for the tunnel interface only. Computers
// without it (or where this program may not change it) report that names are unavailable.
type linuxNames struct{}

func newNameClient() agent.NameClient { return linuxNames{} }

func run(ctx context.Context, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (linuxNames) Apply(ctx context.Context, iface, domain, server string) error {
	if _, err := exec.LookPath("resolvectl"); err != nil {
		return errors.New("this computer does not use systemd-resolved")
	}
	if out, _ := exec.CommandContext(ctx, "systemctl", "is-active", "systemd-resolved").Output(); strings.TrimSpace(string(out)) != "active" {
		return errors.New("systemd-resolved is not running on this computer")
	}
	cmds, err := resolvectlApply(iface, domain, server)
	if err != nil {
		return err
	}
	for _, c := range cmds {
		if err := run(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func (linuxNames) Clear(ctx context.Context, iface string) error {
	if _, err := exec.LookPath("resolvectl"); err != nil || !ifaceRE.MatchString(iface) {
		return nil
	}
	_ = run(ctx, resolvectlClear(iface)) // fails harmlessly when the interface is already gone, which clears it too
	return nil
}
