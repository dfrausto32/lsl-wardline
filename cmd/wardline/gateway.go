package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/gateway"
	wlog "lsl-wardline/internal/log"
	"lsl-wardline/internal/wg"
	"lsl-wardline/internal/wintun"
)

// gatewayOpts are the flags the server role commands share.
type gatewayOpts struct {
	controlURL, token, stateDir, iface, name, backend, level string
}

// defaultServerDir is where the server keeps its identity.
func defaultServerDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(programData(), "Wardline-server")
	}
	return "/var/lib/wardline"
}

// buildGateway makes the server role from its flags.
func buildGateway(o *gatewayOpts) (*gateway.Gateway, error) {
	log := wlog.New(os.Stderr, o.level)
	if o.controlURL == "" {
		return nil, errors.New("the control plane address is missing (--control-url or WARDLINE_CONTROL_URL)")
	}
	b, kind, err := chooseBackend(o.backend)
	if err != nil {
		return nil, err
	}
	log.Info("using WireGuard backend", "backend", kind)
	name := o.name
	if name == "" {
		name, _ = os.Hostname()
	}
	return gateway.New(gateway.Config{
		StateDir: o.stateDir, Iface: o.iface, Name: name, EnrollToken: o.token, ControlURL: o.controlURL,
		Control: gateway.NewHTTPControl(o.controlURL, &http.Client{}), Backend: b, Log: log,
	}), nil
}

func gatewayCmd() *cobra.Command {
	o := &gatewayOpts{}
	c := &cobra.Command{
		Use:   "gateway",
		Short: "Run the server role: accept devices and report who is connected",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g, err := buildGateway(o)
			if err != nil {
				return err
			}
			// Started by the Windows service manager, it runs under that instead.
			if handled, serr := runAsService(gatewayServiceName, g.Run); handled {
				return serr
			}
			err = g.Run(cmd.Context())
			if errors.Is(err, cmd.Context().Err()) {
				return nil
			}
			return err
		},
	}
	f := c.PersistentFlags() // subcommands such as install share these
	f.StringVar(&o.controlURL, "control-url", envOr("WARDLINE_CONTROL_URL", ""), "address of the control plane")
	f.StringVar(&o.token, "token", envOr("WARDLINE_ENROLL_TOKEN", ""), "one-time install code (first start only)")
	f.StringVar(&o.stateDir, "state-dir", envOr("WARDLINE_STATE_DIR", defaultServerDir()), "folder where this server keeps its identity")
	f.StringVar(&o.iface, "interface", envOr("WARDLINE_INTERFACE", "wardline0"), "name of the network interface to create")
	f.StringVar(&o.name, "name", envOr("WARDLINE_SERVER_NAME", ""), "name shown for this server")
	f.StringVar(&o.backend, "backend", envOr("WARDLINE_BACKEND", "auto"), "auto, kernel or userspace")
	f.StringVar(&o.level, "log-level", envOr("WARDLINE_LOG_LEVEL", "info"), "debug, info, warn or error")
	platformGatewayCommands(c, o)
	return c
}

// chooseBackend picks the kernel implementation when this machine has it and
// falls back to the userspace one, which works anywhere with a TUN device.
func chooseBackend(want string) (wg.Backend, string, error) {
	switch want {
	case "kernel":
		k, err := wg.NewKernel()
		return k, "kernel", err
	case "userspace":
		return userspace()
	case "auto", "":
		// Windows has no kernel WireGuard; the program carries its own (see docs/WINDOWS-TUNNEL.md).
		if runtime.GOOS == "windows" {
			return userspace()
		}
		if wg.KernelAvailable() {
			if k, err := wg.NewKernel(); err == nil {
				return k, "kernel", nil
			}
		}
		return wg.NewUserspace(), "userspace", nil
	}
	return nil, "", fmt.Errorf("unknown backend %q (use auto, kernel or userspace)", want)
}

// userspace runs WireGuard inside this program. On Windows that needs the
// bundled network driver library placed next to the program first.
func userspace() (wg.Backend, string, error) {
	if err := wintun.Prepare(); err != nil {
		return nil, "", err
	}
	return wg.NewUserspace(), "userspace", nil
}
