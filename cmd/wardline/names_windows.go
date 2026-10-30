//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"lsl-wardline/internal/agent"
)

// windowsNames adds one rule to Windows' name settings: names ending in the network's ending go to the
// Wardline server. Everything else keeps using the computer's normal name servers.
type windowsNames struct{}

func newNameClient() agent.NameClient { return windowsNames{} }

func powershell(ctx context.Context, script string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // no console window
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (windowsNames) Apply(ctx context.Context, _ string, domain, server string) error {
	s, err := nrptApplyScript(domain, server)
	if err != nil {
		return err
	}
	return powershell(ctx, s)
}

func (windowsNames) Clear(ctx context.Context, _ string) error {
	return powershell(ctx, nrptClearScript())
}
