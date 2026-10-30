//go:build !windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

const linuxAgentService = "wardline-agent"

// serviceArgs is the systemctl command for the agent service, with sudo in front when this is not root.
func serviceArgs(action string, euid int) []string {
	args := []string{"systemctl", action, linuxAgentService}
	if euid != 0 {
		args = append([]string{"sudo"}, args...)
	}
	return args
}

func setConnection(out io.Writer, on bool) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("this only works where Wardline was installed with its install script (a systemd service)")
	}
	action := "stop"
	if on {
		action = "start"
	}
	args := serviceArgs(action, os.Geteuid())
	c := exec.Command(args[0], args[1:]...)
	c.Stdout, c.Stderr, c.Stdin = out, out, os.Stdin // sudo may ask for a password
	if err := c.Run(); err != nil {
		return errors.New("could not " + action + " the Wardline service: " + err.Error())
	}
	return nil
}

func connectionStatus() (string, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return "", errors.New("this only works where Wardline was installed with its install script (a systemd service)")
	}
	b, _ := exec.Command("systemctl", "is-active", linuxAgentService).Output() // non-zero exit just means "not active"
	switch strings.TrimSpace(string(b)) {
	case "active":
		return "On. This computer is on the private network.", nil
	case "activating":
		return "Connecting…", nil
	}
	return "Off. This computer is not on the private network. Turn it on with: wardline on", nil
}
