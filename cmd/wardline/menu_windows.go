//go:build windows

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"

	"lsl-wardline/internal/agent"
)

func isAdmin() bool { return windows.GetCurrentProcessToken().IsElevated() }

// relaunchAsAdmin starts this program again behind the "Run as administrator" prompt.
func relaunchAsAdmin() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return windows.ShellExecute(0, verb, file, nil, dir, windows.SW_NORMAL)
}

// platformRoot is what double-clicking the program does: ask for permission, then show the menu.
func platformRoot(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	if !isAdmin() {
		fmt.Fprintln(out, "Wardline needs your permission to change this computer. Windows will ask next.")
		if err := relaunchAsAdmin(); err != nil {
			fmt.Fprintln(out, "\nWardline was not given permission, so nothing was changed.")
			waitForEnter(os.Stdin, out)
		}
		return nil
	}
	in := bufio.NewReader(os.Stdin)
	if err := runMenu(cmd.Context(), in, out, realMenuActions()); err != nil {
		fmt.Fprintln(out, "\nSomething went wrong:", err)
	}
	waitForEnter(in, out)
	return nil
}

// waitForEnter keeps the window open so the last message can be read.
func waitForEnter(in io.Reader, out io.Writer) {
	fmt.Fprint(out, "\nPress Enter to close this window.")
	bufio.NewReader(in).ReadString('\n')
}

// realMenuActions connects the menu to the same install steps the commands use.
func realMenuActions() menuActions {
	return menuActions{
		installServer: func(ctx context.Context, out io.Writer) (string, error) {
			return installControlService(ctx, out, &controlOpts{listen: ":8080", data: defaultControlDir(), level: "info"})
		},
		installGateway: func(ctx context.Context, out io.Writer, controlURL, token string) error {
			return installGatewayService(ctx, out, &gatewayOpts{controlURL: controlURL, token: token, stateDir: defaultServerDir(), iface: "wardline0", backend: "auto", level: "info"})
		},
		installAgent: func(ctx context.Context, out io.Writer, controlURL, token string) error {
			o := &agentOpts{controlURL: controlURL, token: token, stateDir: defaultAgentDir(), iface: "wardline-agent", backend: "auto", level: "info"}
			return installAgentService(ctx, out, o, func() (*agent.Agent, error) { return buildAgent(o, 0) })
		},
		remove: func(_ context.Context, out io.Writer) error {
			services := []struct {
				name  string
				rules []string
			}{{gatewayServiceName, []string{serverRuleName}}, {controlServiceName, []string{webRuleName}}, {agentServiceName, nil}}
			for _, s := range services {
				if err := uninstallWinService(s.name, s.rules...); err != nil {
					return err
				}
			}
			if err := os.RemoveAll(defaultAgentDir()); err != nil {
				return err
			}
			fmt.Fprintln(out, "\nWardline was removed from this computer.")
			fmt.Fprintf(out, "If it was your server, its data is still in %s and %s. Remove this computer in the Wardline web app too, so its access ends there.\n", defaultControlDir(), defaultServerDir())
			return nil
		},
	}
}
