//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// installControlService installs the web app and returns the address other computers use for it.
func installControlService(ctx context.Context, out io.Writer, o *controlOpts) (string, error) {
	url := o.publicURL
	if url == "" {
		ip, err := outboundIP()
		if err != nil {
			return "", errors.New("could not work out this computer's address; add --public-url http://<this computer's address>:8080")
		}
		if url, err = publicURLFor(ip, o.listen); err != nil {
			return "", err
		}
	}
	args := controlServiceArgs(o.listen, o.data, url)
	lock := []string{o.data}
	rules := controlFirewall(o.listen)
	if o.withServer { // this computer is also the server, so it needs its own folder and its own port open
		args = append(args, localServerArgs(o.serverDir)...)
		lock = append(lock, o.serverDir)
		rules = append(rules, gatewayFirewall(gatewayPort)...)
	}
	err := installWinService(ctx, out, winSvc{
		name: controlServiceName, display: "Wardline web app", desc: "The Wardline web app and database for your private network.",
		args:     args,
		lockDirs: lock,
		firewall: rules,
		done: func() string {
			return fmt.Sprintf("The Wardline web app is running.\nOn this computer, open http://localhost%s to set up your network.\nOther computers on your network use %s", o.listen, url)
		},
	})
	if err == nil {
		if serr := writeDesktopShortcut("http://localhost" + o.listen); serr == nil {
			fmt.Fprintln(out, "A Wardline icon was added to your desktop.")
		}
	}
	return url, err
}

func installGatewayService(ctx context.Context, out io.Writer, o *gatewayOpts) error {
	if o.controlURL == "" || o.token == "" {
		return errors.New("both --control-url and --token are needed; copy the whole command from the Wardline setup page")
	}
	name := o.name
	if name == "" {
		name, _ = os.Hostname()
	}
	return installWinService(ctx, out, winSvc{
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
			fmt.Fprintln(out, "This computer is registered as your server.")
			return nil
		},
		done: func() string {
			return "The Wardline server is running. Your setup page will show it online within a minute."
		},
	})
}

func platformControlCommands(c *cobra.Command, o *controlOpts) {
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Run the Wardline web app on this computer, starting with Windows (run as administrator)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := installControlService(cmd.Context(), cmd.OutOrStdout(), o)
			return err
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the Wardline web app service (run as administrator). The database is kept.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := uninstallWinService(controlServiceName, webRuleName, serverRuleName); err != nil {
				return err
			}
			removeDesktopShortcut()
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
			return installGatewayService(cmd.Context(), cmd.OutOrStdout(), o)
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
