//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"lsl-wardline/internal/update"
)

var wardlineRoles = []string{"allinone", "control", "gateway", "agent"}

type systemdHost struct{ dir string }

func systemHost() (update.Host, update.Paths, error) {
	if os.Geteuid() != 0 {
		return nil, update.Paths{}, errors.New("updating needs administrator rights: run it with sudo")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil, update.Paths{}, errors.New("updating only works where Wardline runs as a systemd service")
	}
	p := update.Paths{
		Binary:     "/usr/local/bin/wardline",
		SystemdDir: "/etc/systemd/system",
		DataDirs:   []string{"/var/lib/wardline-control", "/var/lib/wardline-gateway", "/var/lib/wardline-agent"},
		StateDir:   "/var/lib/wardline-update",
	}
	if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
		return nil, p, err
	}
	return &systemdHost{dir: p.SystemdDir}, p, nil
}

func (h *systemdHost) Units() []string {
	var out []string
	for _, r := range wardlineRoles {
		if _, err := os.Stat(h.dir + "/wardline-" + r + ".service"); err == nil {
			out = append(out, "wardline-"+r)
		}
	}
	return out
}

func systemctl(args ...string) error {
	if b, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	return nil
}

func (h *systemdHost) DaemonReload() error  { return systemctl("daemon-reload") }
func (h *systemdHost) Stop(u string) error  { return systemctl("stop", u) }
func (h *systemdHost) Start(u string) error { return systemctl("start", u) }

// Healthy waits for every Wardline service to be running, the web app (if this computer has one) to answer,
// and then checks again a little later to catch a service that starts and falls over.
func (h *systemdHost) Healthy(ctx context.Context) error {
	units := h.Units()
	web := ""
	for _, u := range units {
		if u == "wardline-allinone" || u == "wardline-control" {
			web = webAddress(strings.TrimPrefix(u, "wardline-"))
		}
	}
	check := func() error {
		for _, u := range units {
			if b, _ := exec.Command("systemctl", "is-active", u).Output(); strings.TrimSpace(string(b)) != "active" {
				return fmt.Errorf("%s is not running", u)
			}
		}
		if web != "" {
			c := &http.Client{Timeout: 5 * time.Second}
			res, err := c.Get("http://" + web + "/api/state")
			if err != nil {
				return fmt.Errorf("the web app did not answer: %w", err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("the web app answered %s", res.Status)
			}
		}
		return nil
	}
	deadline := time.Now().Add(90 * time.Second)
	var err error
	for {
		if err = check(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(20 * time.Second):
	}
	return check()
}

// webAddress is where the web app listens on this computer, from its settings file.
func webAddress(role string) string {
	port := "8080"
	if f, err := os.Open("/etc/wardline/" + role + ".env"); err == nil {
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			if v, ok := strings.CutPrefix(s.Text(), "WARDLINE_LISTEN="); ok {
				if _, p, err := net.SplitHostPort(strings.TrimSpace(v)); err == nil && p != "" {
					port = p
				}
			}
		}
	}
	return "127.0.0.1:" + port
}
