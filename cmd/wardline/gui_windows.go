//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jchv/go-webview2"
	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"

	"lsl-wardline/internal/agent"
)

// trace notes how far the installer got in a small file, because a program with no console has nowhere else to say.
// Look in the Temp folder (type %TEMP% in Explorer) for wardline-installer.log.
func trace(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(os.TempDir(), "wardline-installer.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05")}, args...)...)
}

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

// tell shows a plain message box; the program has no console window to print to.
func tell(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	title, _ := windows.UTF16PtrFromString("Wardline")
	windows.MessageBox(0, t, title, windows.MB_OK)
}

// platformRoot is what double-clicking the program does: ask for permission, then open the installer window.
func platformRoot(cmd *cobra.Command) error {
	trace("started, administrator=%v", isAdmin())
	// A computer that has already joined the network gets its status window, which needs no permission to look at.
	if serviceExists(agentServiceName) {
		trace("this computer has joined; opening the status window")
		if err := runStatusWindow(cmd.Context()); err != nil {
			trace("status window failed: %v", err)
			tell("Wardline could not open its window: " + err.Error())
		}
		return nil
	}
	if !isAdmin() {
		if err := relaunchAsAdmin(); err != nil {
			trace("asking for permission failed: %v", err)
			tell("Wardline was not given permission, so nothing was changed.")
		}
		return nil
	}
	trace("opening the installer window")
	if err := runInstallerWindow(cmd.Context()); err != nil {
		trace("window failed: %v", err)
		tell("Wardline could not open its installer window: " + err.Error())
	}
	return nil
}

// runInstallerWindow shows the installer in a native window. The page it shows is served by this
// program on this computer only, and the window navigates on to the Wardline web app afterwards.
func runInstallerWindow(ctx context.Context) error {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	makeSharp()
	key := hex.EncodeToString(raw)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()

	closed := make(chan struct{})
	var w webview2.WebView
	quit := func() {
		if w != nil {
			w.Dispatch(w.Terminate)
			return
		}
		select {
		case <-closed:
		default:
			close(closed)
		}
	}
	h := installerHandler(key, "http://localhost:8080", realInstallActions(), quit, openInBrowser)
	go http.Serve(ln, h)
	url := fmt.Sprintf("http://127.0.0.1:%d/?k=%s", ln.Addr().(*net.TCPAddr).Port, key)

	trace("listening on %s", ln.Addr())
	w = webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:      filepath.Join(os.TempDir(), "Wardline-installer"),
		AutoFocus:     true,
		WindowOptions: webview2.WindowOptions{Title: "Wardline", Width: 640, Height: 760, IconId: 1, Center: true},
	})
	if w == nil {
		trace("the built-in web view could not start, using the browser")
		// The built-in web view is missing (an old Windows 10); use the web browser instead.
		if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
			return err
		}
		select {
		case <-closed:
		case <-ctx.Done():
		}
		return nil
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
	return nil
}

// realInstallActions connects the installer to the same install steps the commands use.
func realInstallActions() installActions {
	return installActions{
		installServer: func(ctx context.Context, out io.Writer) (string, error) {
			return installControlService(ctx, out, &controlOpts{listen: ":8080", data: defaultControlDir(), level: "info", withServer: true, serverDir: defaultServerDir()})
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
			}{{gatewayServiceName, []string{serverRuleName}}, {controlServiceName, []string{webRuleName, serverRuleName}}, {agentServiceName, nil}}
			for _, s := range services {
				if err := uninstallWinService(s.name, s.rules...); err != nil {
					return err
				}
			}
			removeDesktopShortcut()
			if err := os.RemoveAll(defaultAgentDir()); err != nil {
				return err
			}
			fmt.Fprintf(out, "If this was your server, its data is still in %s and %s. Remove this computer in the Wardline web app too, so its access ends there.", defaultControlDir(), defaultServerDir())
			return nil
		},
	}
}

// makeSharp tells Windows this program draws its own pixels at any screen scaling. Without it, Windows
// enlarges the window as a picture and everything looks blurry on a scaled or high-resolution screen.
func makeSharp() {
	user32 := syscall.NewLazyDLL("user32.dll")
	// -4 is "per-monitor aware v2" (Windows 10 1703 and later); fall back to the older call.
	if ok, _, _ := user32.NewProc("SetProcessDpiAwarenessContext").Call(^uintptr(3)); ok != 0 {
		return
	}
	user32.NewProc("SetProcessDPIAware").Call()
}
