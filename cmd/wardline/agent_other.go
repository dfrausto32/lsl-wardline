//go:build !windows

package main

import (
	"context"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/agent"
)

// runAsService is only meaningful on Windows, where the service manager starts the program.
func runAsService(func(ctx context.Context) error) (bool, error) { return false, nil }

// platformAgentCommands adds commands that exist on this system only. On Linux the install script does this.
func platformAgentCommands(*cobra.Command, *agentOpts, func() (*agent.Agent, error)) {}
