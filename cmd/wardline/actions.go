package main

import (
	"context"
	"io"
	"strings"
)

// installActions are the install steps the Windows installer window offers. Each runs the same steps
// as the matching command; keeping them behind this type lets the window's logic be tested anywhere.
type installActions struct {
	// installServer sets up the web app (and the server in it) and returns the address other computers use.
	installServer  func(ctx context.Context, out io.Writer) (webURL string, err error)
	installGateway func(ctx context.Context, out io.Writer, controlURL, token string) error
	installAgent   func(ctx context.Context, out io.Writer, controlURL, token string) error
	remove         func(ctx context.Context, out io.Writer) error
}

// parseInstallCommand reads --control-url and --token out of a command pasted from the web app.
// The code is required; the address may be missing.
func parseInstallCommand(s string) (controlURL, token string, ok bool) {
	fields := strings.Fields(s)
	value := func(name string) string {
		for i, f := range fields {
			var v string
			switch {
			case f == name && i+1 < len(fields):
				v = fields[i+1]
			case strings.HasPrefix(f, name+"="):
				v = strings.TrimPrefix(f, name+"=")
			default:
				continue
			}
			return strings.Trim(v, `"'`)
		}
		return ""
	}
	controlURL, token = value("--control-url"), value("--token")
	return controlURL, token, token != ""
}
