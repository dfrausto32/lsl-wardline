//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"lsl-wardline/internal/agent"
)

func installAgentService(ctx context.Context, out io.Writer, o *agentOpts, build func() (*agent.Agent, error)) error {
	// A computer that has already joined can be updated in place without a new code.
	if known, ok := joinedControlURL(o.stateDir); ok {
		if o.controlURL == "" {
			o.controlURL = known
		}
	} else if o.controlURL == "" || o.token == "" {
		return errors.New("both --control-url and --token are needed; copy the whole command from Wardline")
	}
	if o.statusFile == "" {
		o.statusFile = defaultStatusFile()
	}
	return installWinService(ctx, out, winSvc{
		name: agentServiceName, display: "Wardline", desc: "Connects this computer to your private network.",
		args:     []string{"agent", "--control-url", o.controlURL, "--state-dir", o.stateDir, "--status-file", o.statusFile},
		lockDirs: []string{o.stateDir},
		setup: func(ctx context.Context) error {
			a, err := build()
			if err != nil {
				return err
			}
			if err := a.Start(ctx); err != nil {
				return err
			}
			fmt.Fprintln(out, "This computer is set up.")
			return nil
		},
		done: func() string {
			return "Wardline is running. This computer will show as Connected in Wardline within a minute."
		},
		after: func(out io.Writer) { setUpMemberExtras(out, o.statusFile) },
	})
}

func uninstallAgentService(out io.Writer, o *agentOpts) error {
	// The tunnel lives inside the service, so stopping it takes the connection down.
	if err := uninstallWinService(agentServiceName); err != nil {
		return err
	}
	statusFile := o.statusFile
	if statusFile == "" {
		statusFile = defaultStatusFile()
	}
	removeMemberExtras(statusFile)
	if err := os.RemoveAll(o.stateDir); err != nil {
		return err
	}
	fmt.Fprintln(out, "Wardline was removed from this computer. Remove it from your network in Wardline too, so its access ends there.")
	return nil
}

// setAgentRunning stops or starts the service, which takes this computer off or back onto the network.
func setAgentRunning(on bool) error {
	m, err := mgr.Connect()
	if err != nil {
		return needAdmin()
	}
	defer m.Disconnect()
	s, err := m.OpenService(agentServiceName)
	if err != nil {
		return errors.New("Wardline is not set up on this computer")
	}
	defer s.Close()
	if on {
		if st, err := s.Query(); err == nil && st.State == svc.Running {
			return nil
		}
		return s.Start()
	}
	stopService(s)
	return nil
}

func platformAgentCommands(c *cobra.Command, o *agentOpts, build func() (*agent.Agent, error)) {
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Set this computer up and start the Wardline service (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return installAgentService(cmd.Context(), cmd.OutOrStdout(), o, build)
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the Wardline service and remove this computer's connection and keys (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return uninstallAgentService(cmd.OutOrStdout(), o)
		},
	}, &cobra.Command{
		Use:   "stop",
		Short: "Take this computer off the network until it is started again (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := setAgentRunning(false); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Wardline is stopped. This computer is off the network.")
			return nil
		},
	}, &cobra.Command{
		Use:   "start",
		Short: "Put this computer back on the network (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := setAgentRunning(true); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Wardline is running.")
			return nil
		},
	})
}

// joinedControlURL reads where an already-joined computer is connected, from its saved identity.
// Only the address is read; the keys in that file are never looked at here.
func joinedControlURL(stateDir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(stateDir, "agent.json"))
	if err != nil {
		return "", false
	}
	var id struct {
		ControlURL string `json:"control_url"`
	}
	if json.Unmarshal(b, &id) != nil || id.ControlURL == "" {
		return "", false
	}
	return id.ControlURL, true
}
