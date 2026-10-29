// Command wardline is the single binary: control plane, server role, and admin tools.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "wardline:", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "wardline",
		Short:         "Private networks for people who do not know networking",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return platformRoot(cmd) },
	}
	root.AddCommand(controlCmd(), gatewayCmd(), agentCmd(), auditCmd(), &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), version) },
	})
	return root
}

// envOr returns the environment variable if set, otherwise the fallback.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
