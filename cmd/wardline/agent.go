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

// programData is the Windows folder for data that belongs to the whole computer.
func programData() string {
	if pd := os.Getenv("ProgramData"); pd != "" {
		return pd
	}
	return `C:\ProgramData`
}

func defaultAgentDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(programData(), "Wardline")
	}
	return "/var/lib/wardline-agent"
}

// agentCmd is the device role: it runs on a computer that joins the network, makes its own key
// and keeps its tunnel matching what the control plane says.
func agentCmd() *cobra.Command {
	o := &agentOpts{}
	controlURL, token, stateDir, iface, backend, level := &o.controlURL, &o.token, &o.stateDir, &o.iface, &o.backend, &o.level
	var refreshCheck time.Duration

	build := func() (*agent.Agent, error) { return buildAgent(o, refreshCheck) }

	c := &cobra.Command{
		Use:   "agent",
		Short: "Run on a computer that joins the network: keeps its own key and stays connected",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := build()
			if err != nil {
				return err
			}
			// Started by the Windows service manager, it runs under that instead.
			if handled, serr := runAsService(agentServiceName, a.Run); handled {
				return serr
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
	pf.StringVar(controlURL, "control-url", envOr("WARDLINE_CONTROL_URL", ""), "address of the control plane")
	pf.StringVar(token, "token", envOr("WARDLINE_ENROLL_TOKEN", ""), "one-time setup code (first start only)")
	pf.StringVar(stateDir, "state-dir", envOr("WARDLINE_STATE_DIR", defaultAgentDir()), "folder where this computer keeps its keys")
	pf.StringVar(iface, "interface", envOr("WARDLINE_INTERFACE", "wardline-agent"), "name of the network interface to create")
	pf.StringVar(backend, "backend", envOr("WARDLINE_BACKEND", "auto"), "auto, kernel or userspace")
	pf.StringVar(level, "log-level", envOr("WARDLINE_LOG_LEVEL", "info"), "debug, info, warn or error")
	c.Flags().DurationVar(&refreshCheck, "refresh-check", 0, "how often to check whether a security refresh is due (default 5m)")

	c.AddCommand(&cobra.Command{
		Use:   "enroll",
		Short: "Set this computer up with the one-time code, then stop",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.token == "" {
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
			if err := agent.RequestRefresh(o.stateDir); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Asked. The security refresh starts within a few minutes.")
			return nil
		},
	})
	platformAgentCommands(c, o, build)
	return c
}

// agentOpts are the flags every agent command shares.
type agentOpts struct {
	controlURL, token, stateDir, iface, backend, level string
}

// buildAgent makes the device role from its flags.
func buildAgent(o *agentOpts, refreshCheck time.Duration) (*agent.Agent, error) {
	if o.controlURL == "" {
		return nil, errors.New("the control plane address is missing (--control-url or WARDLINE_CONTROL_URL)")
	}
	b, kind, err := chooseBackend(o.backend)
	if err != nil {
		return nil, err
	}
	log := wlog.New(os.Stderr, o.level)
	log.Info("using WireGuard backend", "backend", kind)
	return agent.New(agent.Config{
		StateDir: o.stateDir, Iface: o.iface, ControlURL: o.controlURL, EnrollToken: o.token, Version: version,
		Control: agent.NewHTTPControl(o.controlURL, &http.Client{}), Backend: b, Log: log, RefreshCheckEvery: refreshCheck,
	}), nil
}
