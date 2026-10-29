package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"lsl-wardline/internal/agent"
)

//go:embed window.html
var windowPage []byte

// windowDeps are the things the desktop window needs, so its logic can be tested without Windows.
type windowDeps struct {
	status func() agent.Status
	now    func() time.Time
	act    func(name string) error // disconnect, connect, refresh, remove
	open   func(url string)
	quit   func()
}

// The only things the window can ask for. Anything else is refused.
var windowActions = map[string]bool{"disconnect": true, "connect": true, "refresh": true, "remove": true}

// windowHandler is the small local page behind the desktop window. It shows how this computer is doing
// and offers a few actions; the actions that change the computer ask Windows for permission themselves.
func windowHandler(key string, d windowDeps) http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		now := d.now()
		s := d.status()
		state, headline := s.Describe(now)
		reply(w, map[string]any{
			"state": string(state), "headline": headline, "connected": state == agent.StateConnected,
			"address": strings.SplitN(s.Address, "/", 2)[0], "server": hostOnly(s.Server),
			"sent": humanBytes(s.TxBytes), "received": humanBytes(s.RxBytes), "version": s.Version,
			"web_url": s.ControlURL, "last_connected": ago(s.LastHandshake, now),
		})
	})
	mux.HandleFunc("/api/action", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in)
		switch {
		case in.Name == "web":
			if u := d.status().ControlURL; strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
				d.open(u)
			}
		case in.Name == "close":
			d.quit()
		case windowActions[in.Name]:
			if err := d.act(in.Name); err != nil {
				reply(w, map[string]any{"ok": false, "message": "That did not work: " + err.Error()})
				return
			}
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		reply(w, map[string]any{"ok": true})
	})
	return guarded(key, windowPage, mux)
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func humanBytes(n int64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.1f KB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	}
	return fmt.Sprintf("%.1f GB", f/(k*k*k))
}

// ago says how long before now a time was, in plain words.
func ago(t, now time.Time) string {
	if t.IsZero() {
		return "not yet"
	}
	d := now.Sub(t)
	unit := func(n int, name string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s ago", name)
		}
		return fmt.Sprintf("%d %ss ago", n, name)
	}
	switch {
	case d < time.Minute:
		return unit(int(d.Seconds()), "second")
	case d < time.Hour:
		return unit(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return unit(int(d.Hours()), "hour")
	}
	return unit(int(d.Hours()/24), "day")
}
