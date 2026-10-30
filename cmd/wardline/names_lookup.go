package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/dnsserver"
)

// namesCmd is for checking names by hand: "wardline names lookup office-pc.wardline.internal 10.77.5.1".
func namesCmd() *cobra.Command {
	c := &cobra.Command{Use: "names", Short: "Check the network's names", Hidden: true}
	c.AddCommand(&cobra.Command{
		Use:   "lookup <name> <server-address>",
		Short: "Ask the Wardline server for a name's address",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			addrs, rcode, err := dnsserver.Query(ctx, net.JoinHostPort(args[1], "53"), args[0])
			if err != nil {
				return err
			}
			switch {
			case rcode == dnsserver.RcodeNameError:
				return fmt.Errorf("%s: no such name", args[0])
			case rcode != dnsserver.RcodeOK:
				return fmt.Errorf("%s: the server refused the question", args[0])
			}
			for _, a := range addrs {
				fmt.Fprintln(cmd.OutOrStdout(), a)
			}
			return nil
		},
	})
	return c
}
