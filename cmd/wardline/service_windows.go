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

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"lsl-wardline/internal/agent"
	"lsl-wardline/internal/wintun"
)

// Well-known account identifiers, the same on every language of Windows.
const (
	sidSystem         = "*S-1-5-18"
	sidAdministrators = "*S-1-5-32-544"
)

// runAsService runs under the Windows service manager when that is what started the program.
func runAsService(name string, run func(ctx context.Context) error) (bool, error) {
	is, err := svc.IsWindowsService()
	if err != nil || !is {
		return false, err
	}
	return true, svc.Run(name, &winService{run: run})
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

func programDir() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "Wardline")
}

// lockDown leaves only the system and administrators with access to a folder that will hold keys.
func lockDown(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
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

func needAdmin() error {
	return errors.New("this needs administrator rights: open Windows Terminal or PowerShell with \"Run as administrator\" and run the command again")
}

// winSvc describes one Windows service Wardline installs.
type winSvc struct {
	name, display, desc string
	args                []string
	lockDirs            []string                    // folders restricted to the system and administrators before anything is stored in them
	firewall            []fwRule                    // inbound rules to add
	setup               func(context.Context) error // first-time setup, before the service exists (enrolling with a one-time code)
	done                func() string               // what to tell the person once it runs
	after               func(out io.Writer)         // best-effort extras once the service runs (shortcuts, tray icon)
}

func stopService(s *mgr.Service) {
	if st, err := s.Query(); err != nil || st.State == svc.Stopped {
		return
	}
	s.Control(svc.Stop)
	for i := 0; i < 30; i++ {
		if st, err := s.Query(); err != nil || st.State == svc.Stopped {
			return
		}
		time.Sleep(time.Second)
	}
}

// installWinService copies the program into place, applies setup, opens the firewall, and creates and starts the service.
// Running it again updates everything in place.
func installWinService(ctx context.Context, out io.Writer, sp winSvc) error {
	say := func(s string) { fmt.Fprintln(out, s) }
	m, err := mgr.Connect()
	if err != nil {
		return needAdmin()
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

	// Every Wardline service runs this same program file, so all of them must stop before it can be
	// replaced. The ones that were running start again afterwards, even if something below fails.
	existing, err := m.OpenService(sp.name)
	if err == nil {
		stopService(existing)
	} else {
		existing = nil
	}
	for _, name := range []string{agentServiceName, controlServiceName, gatewayServiceName} {
		if name == sp.name {
			continue
		}
		if other, err := m.OpenService(name); err == nil {
			if st, qerr := other.Query(); qerr == nil && st.State == svc.Running {
				stopService(other)
				defer func(o *mgr.Service) { o.Start(); o.Close() }(other)
				continue
			}
			other.Close()
		}
	}
	removeAside(exe)
	if self != exe {
		if err := replaceFile(exe, func(p string) error { return copyFile(self, p) }); err != nil {
			return fmt.Errorf("could not copy the program into place: %w", err)
		}
	}
	// The network driver library the program carries goes next to the installed copy.
	if err := wintun.PrepareIn(programDir()); err != nil {
		return err
	}
	for _, d := range sp.lockDirs {
		if err := lockDown(d); err != nil {
			return err
		}
	}
	for _, r := range sp.firewall {
		_ = exec.Command("netsh", firewallDeleteArgs(r.Name)...).Run() // replace, never stack
		if out, err := exec.Command("netsh", firewallAddArgs(r)...).CombinedOutput(); err != nil {
			return fmt.Errorf("could not open the firewall for %s: %w: %s", r.Name, err, out)
		}
	}
	if sp.setup != nil {
		if err := sp.setup(ctx); err != nil {
			return err
		}
	}

	cfg := mgr.Config{DisplayName: sp.display, Description: sp.desc, StartType: mgr.StartAutomatic}
	var s *mgr.Service
	if existing != nil {
		s = existing
		cfg.BinaryPathName = windows.ComposeCommandLine(append([]string{exe}, sp.args...))
		if err := s.UpdateConfig(cfg); err != nil {
			return err
		}
	} else if s, err = m.CreateService(sp.name, exe, cfg, sp.args...); err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
			return errors.New("Windows is still removing the old Wardline. Close the Services window and Task Manager if they are open, wait a few seconds, and try again")
		}
		return fmt.Errorf("could not create the %s service: %w", sp.display, err)
	}
	defer s.Close()
	// Come back by itself if it ever stops for a real problem.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}}, 86400)
	if err := s.Start(); err != nil {
		return fmt.Errorf("could not start the %s service: %w", sp.display, err)
	}
	say(sp.done())
	if sp.after != nil {
		sp.after(out)
	}
	return nil
}

// uninstallWinService stops and deletes the service and closes its firewall rules. Data folders are left to the caller.
func uninstallWinService(name string, rules ...string) error {
	m, err := mgr.Connect()
	if err != nil {
		return needAdmin()
	}
	defer m.Disconnect()
	if s, err := m.OpenService(name); err == nil {
		stopService(s)
		s.Delete()
		s.Close()
	}
	for _, r := range rules {
		_ = exec.Command("netsh", firewallDeleteArgs(r)...).Run()
	}
	return nil
}
