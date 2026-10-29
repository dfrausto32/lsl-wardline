//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// The server's WireGuard port; the web app tells devices to use it.
const gatewayPort = 51820

func platformControlCommands(c *cobra.Command, o *controlOpts) {
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Run the Wardline web app on this computer, starting with Windows (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url := o.publicURL
			if url == "" {
				ip, err := outboundIP()
				if err != nil {
					return errors.New("could not work out this computer's address; add --public-url http://<this computer's address>:8080")
				}
				if url, err = publicURLFor(ip, o.listen); err != nil {
					return err
				}
			}
			return installWinService(cmd.Context(), cmd, winSvc{
				name: controlServiceName, display: "Wardline web app", desc: "The Wardline web app and database for your private network.",
				args:     controlServiceArgs(o.listen, o.data, url),
				lockDirs: []string{o.data},
				firewall: controlFirewall(o.listen),
				done: func() string {
					return fmt.Sprintf("The Wardline web app is running.\nOn this computer, open http://localhost%s to set up your network.\nOther computers on your network use %s", o.listen, url)
				},
			})
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the Wardline web app service (run as administrator). The database is kept.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := uninstallWinService(controlServiceName, webRuleName); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "The Wardline web app was stopped and removed. Your data is still in %s.\n", o.data)
			return nil
		},
	})
}

func platformGatewayCommands(c *cobra.Command, o *gatewayOpts) {
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Make this computer your Wardline server, starting with Windows (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.controlURL == "" || o.token == "" {
				return errors.New("both --control-url and --token are needed; copy the whole command from the Wardline setup page")
			}
			name := o.name
			if name == "" {
				name, _ = os.Hostname()
			}
			return installWinService(cmd.Context(), cmd, winSvc{
				name: gatewayServiceName, display: "Wardline server", desc: "Lets your devices connect to your private network.",
				args:     gatewayServiceArgs(o.controlURL, o.stateDir, o.iface, name),
				lockDirs: []string{o.stateDir},
				firewall: gatewayFirewall(gatewayPort),
				// The one-time code is used here, once, and is not kept in the service settings.
				setup: func(ctx context.Context) error {
					g, err := buildGateway(o)
					if err != nil {
						return err
					}
					if err := g.Start(ctx); err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "This computer is registered as your server.")
					return nil
				},
				done: func() string {
					return "The Wardline server is running. Your setup page will show it online within a minute."
				},
			})
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the Wardline server service (run as administrator). Its saved identity is kept.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := uninstallWinService(gatewayServiceName, serverRuleName); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "The Wardline server was stopped and removed. Its identity is still in %s.\n", o.stateDir)
			return nil
		},
	})
}
