//go:build !windows

package main

import (
	"context"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/agent"
)

// runAsService is only meaningful on Windows, where the service manager starts the program.
func runAsService(string, func(ctx context.Context) error) (bool, error) { return false, nil }

// The platform commands add install steps that exist on this system only. On Linux the install script does this.
func platformAgentCommands(*cobra.Command, *agentOpts, func() (*agent.Agent, error)) {}
func platformControlCommands(*cobra.Command, *controlOpts)                           {}
func platformGatewayCommands(*cobra.Command, *gatewayOpts)                           {}
