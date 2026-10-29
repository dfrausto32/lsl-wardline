package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// The double-click menu for people who would rather answer questions than type commands. It only
// asks and calls the same install steps the commands use; menuActions keeps it testable anywhere.

type menuActions struct {
	// installServer sets up the web app and returns the address other computers use for it.
	installServer  func(ctx context.Context, out io.Writer) (webURL string, err error)
	installGateway func(ctx context.Context, out io.Writer, controlURL, token string) error
	installAgent   func(ctx context.Context, out io.Writer, controlURL, token string) error
	remove         func(ctx context.Context, out io.Writer) error
}

// readLine returns one trimmed line and false when the input has ended.
func readLine(in *bufio.Reader) (string, bool) {
	s, err := in.ReadString('\n')
	if err != nil && s == "" {
		return "", false
	}
	return strings.TrimSpace(s), true
}

func runMenu(ctx context.Context, in *bufio.Reader, out io.Writer, a menuActions) error {
	say := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	failed := func(err error) { say("\nThat did not work: %v", err) }

	say("Wardline")
	say("Private networks that anyone can set up.\n")
	for {
		say("What would you like to do?")
		say("  1) Make this computer my server")
		say("  2) Add this computer to my network")
		say("  3) Remove Wardline from this computer")
		say("  4) Quit")
		fmt.Fprint(out, "\nType 1, 2, 3 or 4 and press Enter: ")
		choice, ok := readLine(in)
		if !ok {
			return nil
		}
		switch choice {
		case "1":
			if err := serverFlow(ctx, in, out, a); err != nil {
				failed(err)
			}
			return nil
		case "2":
			if err := addFlow(ctx, in, out, a); err != nil {
				failed(err)
			}
			return nil
		case "3":
			fmt.Fprint(out, "\nThis stops Wardline on this computer and removes its connection. Continue? (y/n): ")
			if yes, _ := readLine(in); strings.EqualFold(yes, "y") || strings.EqualFold(yes, "yes") {
				if err := a.remove(ctx, out); err != nil {
					failed(err)
				}
			} else {
				say("Nothing was changed.")
			}
			return nil
		case "4":
			return nil
		default:
			say("\nPlease type 1, 2, 3 or 4.\n")
		}
	}
}

func serverFlow(ctx context.Context, in *bufio.Reader, out io.Writer, a menuActions) error {
	say := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	say("\nStep 1 of 2: starting the Wardline web app on this computer...")
	webURL, err := a.installServer(ctx, out)
	if err != nil {
		return err
	}
	say("\nStep 2 of 2: set up your network in the web app.")
	say("  - On this computer, open your web browser at the address above and follow the screens.")
	say("  - When it shows the server command, choose the \"Windows service\" tab and copy it.")
	fmt.Fprint(out, "\nPaste that command here and press Enter (or just press Enter to finish this later): ")
	for {
		line, ok := readLine(in)
		if !ok || line == "" {
			say("\nTo finish later, open PowerShell as administrator and run the command from the web app,")
			say("it starts with:  .\\wardline.exe gateway install")
			return nil
		}
		url, token, found := parseInstallCommand(line)
		if !found && !strings.ContainsAny(line, " \t") {
			url, token, found = "", line, true // just the one-time code
		}
		if !found {
			fmt.Fprint(out, "I could not find the code in that. Paste the whole command, then press Enter: ")
			continue
		}
		if url == "" {
			url = webURL
		}
		if err := a.installGateway(ctx, out, url, token); err != nil {
			return err
		}
		say("\nDone. Your setup page will show the server online within a minute.")
		return nil
	}
}

func addFlow(ctx context.Context, in *bufio.Reader, out io.Writer, a menuActions) error {
	fmt.Fprint(out, "\nIn the Wardline web app choose Add a device, then Windows, and copy the command it shows.\nPaste that command here and press Enter: ")
	for {
		line, ok := readLine(in)
		if !ok || line == "" {
			fmt.Fprintln(out, "\nNothing was changed.")
			return nil
		}
		url, token, found := parseInstallCommand(line)
		if !found || url == "" {
			fmt.Fprint(out, "I could not find the address and code in that. Paste the whole command, then press Enter: ")
			continue
		}
		if err := a.installAgent(ctx, out, url, token); err != nil {
			return err
		}
		fmt.Fprintln(out, "\nDone. This computer will show as Connected in Wardline within a minute.")
		return nil
	}
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
