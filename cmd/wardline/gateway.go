package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"lsl-wardline/internal/gateway"
	wlog "lsl-wardline/internal/log"
	"lsl-wardline/internal/wg"
)

func gatewayCmd() *cobra.Command {
	var controlURL, token, stateDir, iface, name, backend, level string
	c := &cobra.Command{
		Use:   "gateway",
		Short: "Run the server role: accept devices and report who is connected",
		RunE: func(cmd *cobra.Command, _ []string) error {
			log := wlog.New(os.Stderr, level)
			if controlURL == "" {
				return errors.New("the control plane address is missing (--control-url or WARDLINE_CONTROL_URL)")
			}
			b, kind, err := chooseBackend(backend)
			if err != nil {
				return err
			}
			log.Info("using WireGuard backend", "backend", kind)
			if name == "" {
				name, _ = os.Hostname()
			}
			g := gateway.New(gateway.Config{
				StateDir: stateDir, Iface: iface, Name: name, EnrollToken: token, ControlURL: controlURL,
				Control: gateway.NewHTTPControl(controlURL, &http.Client{}), Backend: b, Log: log,
			})
			err = g.Run(cmd.Context())
			if errors.Is(err, cmd.Context().Err()) {
				return nil
			}
			return err
		},
	}
	f := c.Flags()
	f.StringVar(&controlURL, "control-url", envOr("WARDLINE_CONTROL_URL", ""), "address of the control plane")
	f.StringVar(&token, "token", envOr("WARDLINE_ENROLL_TOKEN", ""), "one-time install code (first start only)")
	f.StringVar(&stateDir, "state-dir", envOr("WARDLINE_STATE_DIR", "/var/lib/wardline"), "folder where this server keeps its identity")
	f.StringVar(&iface, "interface", envOr("WARDLINE_INTERFACE", "wardline0"), "name of the network interface to create")
	f.StringVar(&name, "name", envOr("WARDLINE_SERVER_NAME", ""), "name shown for this server")
	f.StringVar(&backend, "backend", envOr("WARDLINE_BACKEND", "auto"), "auto, kernel or userspace")
	f.StringVar(&level, "log-level", envOr("WARDLINE_LOG_LEVEL", "info"), "debug, info, warn or error")
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
		return wg.NewUserspace(), "userspace", nil
	case "auto", "":
		if wg.KernelAvailable() {
			if k, err := wg.NewKernel(); err == nil {
				return k, "kernel", nil
			}
		}
		return wg.NewUserspace(), "userspace", nil
	}
	return nil, "", fmt.Errorf("unknown backend %q (use auto, kernel or userspace)", want)
}
