package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// onOffCommands are the short commands for taking this computer off the private network and putting it back:
// wardline off, wardline on, wardline status. They work the same on Windows and Linux.
func onOffCommands() []*cobra.Command {
	return []*cobra.Command{
		{
			Use:   "off",
			Short: "Turn the private network connection off on this computer",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := setConnection(cmd.OutOrStdout(), false); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Off. This computer is not on the private network. Turn it back on with: wardline on")
				return nil
			},
		},
		{
			Use:   "on",
			Short: "Turn the private network connection on again",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := setConnection(cmd.OutOrStdout(), true); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "On. This computer is joining the private network. Check with: wardline status")
				return nil
			},
		},
		{
			Use:   "status",
			Short: "Say whether this computer is on the private network",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				text, err := connectionStatus()
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), text)
				return nil
			},
		},
	}
}
