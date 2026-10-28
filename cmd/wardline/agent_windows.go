//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"lsl-wardline/internal/agent"
	"lsl-wardline/internal/wg"
)

const (
	serviceName = "WardlineAgent"
	// Well-known account identifiers, the same on every language of Windows.
	sidSystem         = "*S-1-5-18"
	sidAdministrators = "*S-1-5-32-544"
)

// runAsService runs the agent under the Windows service manager when that is what started it.
func runAsService(run func(ctx context.Context) error) (bool, error) {
	is, err := svc.IsWindowsService()
	if err != nil || !is {
		return false, err
	}
	return true, svc.Run(serviceName, &winService{run: run})
}

type winService struct {
	run func(ctx context.Context) error
}

func (w *winService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-requests:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		case err := <-done:
			// A removed computer ends cleanly, so the service manager does not start it again.
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, agent.ErrRevoked) {
				return false, 1
			}
			return false, 0
		}
	}
}

func platformAgentCommands(c *cobra.Command, o *agentOpts, build func() (*agent.Agent, error)) {
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Set this computer up and start the Wardline service (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.controlURL == "" || o.token == "" {
				return errors.New("both --control-url and --token are needed; copy the whole command from Wardline")
			}
			return installService(cmd.Context(), cmd, o, build)
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the Wardline service and remove this computer's connection and keys (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return uninstallService(cmd.Context(), cmd, o)
		},
	})
}

func programDir() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "Wardline")
}

// lockDown leaves only the system and administrators with access to a folder that will hold keys.
func lockDown(dir string) error {
	out, err := exec.Command("icacls", dir, "/inheritance:r", "/grant:r", sidSystem+":(OI)(CI)F", "/grant:r", sidAdministrators+":(OI)(CI)F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not restrict access to %s: %w: %s", dir, err, out)
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func installService(ctx context.Context, cmd *cobra.Command, o *agentOpts, build func() (*agent.Agent, error)) error {
	say := func(s string) { fmt.Fprintln(cmd.OutOrStdout(), s) }
	// The tunnel is run by the official WireGuard program; Wardline does not install it for you.
	app := wg.NewWireGuardApp(filepath.Join(o.stateDir, "tunnels"))
	if !app.Installed() {
		return errors.New("WireGuard for Windows is not installed on this computer. Install it from https://www.wireguard.com/install/ and then run this command again")
	}
	m, err := mgr.Connect()
	if err != nil {
		return errors.New("this needs administrator rights: open Windows Terminal or PowerShell with \"Run as administrator\" and run the command again")
	}
	defer m.Disconnect()

	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(programDir(), 0o755); err != nil {
		return err
	}
	exe := filepath.Join(programDir(), "wardline.exe")

	// An older copy that is running must stop before it can be replaced.
	existing, err := m.OpenService(serviceName)
	if err == nil {
		if st, qerr := existing.Query(); qerr == nil && st.State != svc.Stopped {
			existing.Control(svc.Stop)
			for i := 0; i < 30; i++ {
				if st, _ := existing.Query(); st.State == svc.Stopped {
					break
				}
				time.Sleep(time.Second)
			}
		}
	}
	if self != exe {
		if err := copyFile(self, exe); err != nil {
			return fmt.Errorf("could not copy the program into place: %w", err)
		}
	}

	if err := os.MkdirAll(o.stateDir, 0o700); err != nil {
		return err
	}
	if err := lockDown(o.stateDir); err != nil {
		return err
	}
	a, err := build()
	if err != nil {
		return err
	}
	if err := a.Start(ctx); err != nil {
		return err
	}
	say("This computer is set up.")

	args := []string{"agent", "--control-url", o.controlURL, "--state-dir", o.stateDir}
	cfg := mgr.Config{DisplayName: "Wardline", Description: "Connects this computer to your private network.", StartType: mgr.StartAutomatic}
	var s *mgr.Service
	if existing != nil {
		s = existing
		cfg.BinaryPathName = fmt.Sprintf(`"%s" agent --control-url %s --state-dir "%s"`, exe, o.controlURL, o.stateDir)
		if err := s.UpdateConfig(cfg); err != nil {
			return err
		}
	} else if s, err = m.CreateService(serviceName, exe, cfg, args...); err != nil {
		return fmt.Errorf("could not create the Wardline service: %w", err)
	}
	defer s.Close()
	// Come back by itself if it ever stops for a real problem.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}}, 86400)
	if err := s.Start(); err != nil {
		return fmt.Errorf("could not start the Wardline service: %w", err)
	}
	say("Wardline is running. This computer will show as Connected in Wardline within a minute.")
	return nil
}

func uninstallService(ctx context.Context, cmd *cobra.Command, o *agentOpts) error {
	m, err := mgr.Connect()
	if err != nil {
		return errors.New("this needs administrator rights: open Windows Terminal or PowerShell with \"Run as administrator\" and run the command again")
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serviceName); err == nil {
		s.Control(svc.Stop)
		for i := 0; i < 30; i++ {
			if st, err := s.Query(); err != nil || st.State == svc.Stopped {
				break
			}
			time.Sleep(time.Second)
		}
		s.Delete()
		s.Close()
	}
	app := wg.NewWireGuardApp(filepath.Join(o.stateDir, "tunnels"))
	_ = app.Remove(ctx, o.iface)
	if err := os.RemoveAll(o.stateDir); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Wardline was removed from this computer. Remove it from your network in Wardline too, so its access ends there.")
	return nil
}
