//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/agent"
)

func installAgentService(ctx context.Context, out io.Writer, o *agentOpts, build func() (*agent.Agent, error)) error {
	if o.controlURL == "" || o.token == "" {
		return errors.New("both --control-url and --token are needed; copy the whole command from Wardline")
	}
	return installWinService(ctx, out, winSvc{
		name: agentServiceName, display: "Wardline", desc: "Connects this computer to your private network.",
		args:     []string{"agent", "--control-url", o.controlURL, "--state-dir", o.stateDir},
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
	})
}

func uninstallAgentService(out io.Writer, o *agentOpts) error {
	// The tunnel lives inside the service, so stopping it takes the connection down.
	if err := uninstallWinService(agentServiceName); err != nil {
		return err
	}
	if err := os.RemoveAll(o.stateDir); err != nil {
		return err
	}
	fmt.Fprintln(out, "Wardline was removed from this computer. Remove it from your network in Wardline too, so its access ends there.")
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
	})
}
