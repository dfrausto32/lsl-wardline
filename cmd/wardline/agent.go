package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/agent"
	wlog "lsl-wardline/internal/log"
)

func defaultAgentDir() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, "Wardline")
		}
		return `C:\ProgramData\Wardline`
	}
	return "/var/lib/wardline-agent"
}

// agentCmd is the device role: it runs on a computer that joins the network, makes its own key
// and keeps its tunnel matching what the control plane says.
func agentCmd() *cobra.Command {
	var controlURL, token, stateDir, iface, backend, level string
	var refreshCheck time.Duration

	build := func() (*agent.Agent, error) {
		if controlURL == "" {
			return nil, errors.New("the control plane address is missing (--control-url or WARDLINE_CONTROL_URL)")
		}
		b, kind, err := chooseBackend(backend)
		if err != nil {
			return nil, err
		}
		log := wlog.New(os.Stderr, level)
		log.Info("using WireGuard backend", "backend", kind)
		return agent.New(agent.Config{
			StateDir: stateDir, Iface: iface, ControlURL: controlURL, EnrollToken: token, Version: version,
			Control: agent.NewHTTPControl(controlURL, &http.Client{}), Backend: b, Log: log, RefreshCheckEvery: refreshCheck,
		}), nil
	}

	c := &cobra.Command{
		Use:   "agent",
		Short: "Run on a computer that joins the network: keeps its own key and stays connected",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := build()
			if err != nil {
				return err
			}
			err = a.Run(cmd.Context())
			if errors.Is(err, agent.ErrRevoked) {
				fmt.Fprintln(cmd.ErrOrStderr(), "This computer was removed from the network. Its connection and keys have been deleted.")
				return nil // a clean exit, so the service does not restart in a loop
			}
			if err != nil && errors.Is(err, cmd.Context().Err()) {
				return nil
			}
			return err
		},
	}
	pf := c.PersistentFlags()
	pf.StringVar(&controlURL, "control-url", envOr("WARDLINE_CONTROL_URL", ""), "address of the control plane")
	pf.StringVar(&token, "token", envOr("WARDLINE_ENROLL_TOKEN", ""), "one-time setup code (first start only)")
	pf.StringVar(&stateDir, "state-dir", envOr("WARDLINE_STATE_DIR", defaultAgentDir()), "folder where this computer keeps its keys")
	pf.StringVar(&iface, "interface", envOr("WARDLINE_INTERFACE", "wardline-agent"), "name of the network interface to create")
	pf.StringVar(&backend, "backend", envOr("WARDLINE_BACKEND", "auto"), "auto, kernel or userspace")
	pf.StringVar(&level, "log-level", envOr("WARDLINE_LOG_LEVEL", "info"), "debug, info, warn or error")
	c.Flags().DurationVar(&refreshCheck, "refresh-check", 0, "how often to check whether a security refresh is due (default 5m)")

	c.AddCommand(&cobra.Command{
		Use:   "enroll",
		Short: "Set this computer up with the one-time code, then stop",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if token == "" {
				return errors.New("the setup code is missing (--token or WARDLINE_ENROLL_TOKEN)")
			}
			a, err := build()
			if err != nil {
				return err
			}
			if err := a.Start(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "This computer is set up. Start the Wardline service to connect.")
			return nil
		},
	}, &cobra.Command{
		Use:   "refresh",
		Short: "Ask the running agent to refresh this computer's security now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := agent.RequestRefresh(stateDir); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Asked. The security refresh starts within a few minutes.")
			return nil
		},
	})
	return c
}
