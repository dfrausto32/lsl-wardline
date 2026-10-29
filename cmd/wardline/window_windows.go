//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jchv/go-webview2"

	"lsl-wardline/internal/agent"
)

// runStatusWindow shows how this computer is doing in a native window, like WireGuard's own: a status
// light, the address and the server, and a few buttons. It needs no administrator rights to look; the
// buttons that change the computer ask Windows for permission when they are pressed.
func runStatusWindow(ctx context.Context) error {
	makeSharp()
	ensureTray() // the tray icon comes along with the window, as the person's own (not elevated) program
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	key := hex.EncodeToString(raw)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()

	statusPath := defaultStatusFile()
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
	d := windowDeps{
		status: func() agent.Status { s, _ := agent.LoadStatus(statusPath); return s },
		now:    time.Now,
		act: func(name string) error {
			switch name {
			case "disconnect":
				return runElevated("agent", "stop")
			case "connect":
				return runElevated("agent", "start")
			case "refresh":
				return runElevated("agent", "refresh")
			case "remove":
				return runElevated("agent", "uninstall")
			}
			return fmt.Errorf("unknown action %q", name)
		},
		open: openInBrowser,
		quit: quit,
	}
	go http.Serve(ln, windowHandler(key, d))
	url := fmt.Sprintf("http://127.0.0.1:%d/?k=%s", ln.Addr().(*net.TCPAddr).Port, key)
	trace("status window on %s", ln.Addr())

	w = webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:      filepath.Join(os.TempDir(), "Wardline-window"),
		AutoFocus:     true,
		WindowOptions: webview2.WindowOptions{Title: "Wardline", Width: 540, Height: 640, IconId: 1, Center: true},
	})
	if w == nil {
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
