package main

import (
	"bytes"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

//go:embed installer.html
var installerPage []byte

// installerHandler is the small web page behind the Windows installer window. It only ever listens on
// this computer, serves the page only to someone holding the key, and runs the same install steps the
// commands run. webURL is where the Wardline web app opens once the server is installed.
func installerHandler(key, webURL string, a installActions, quit func(), openBrowser func(url string)) http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, ok bool, message, open string) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": ok, "message": strings.TrimSpace(message), "open": open})
	}
	// run turns an install step into the reply the page shows: what it said, or what went wrong.
	run := func(w http.ResponseWriter, r *http.Request, open string, step func(out *bytes.Buffer) error) {
		var out bytes.Buffer
		if err := step(&out); err != nil {
			reply(w, false, "That did not work: "+err.Error(), "")
			return
		}
		reply(w, true, out.String(), open)
	}
	mux.HandleFunc("/api/server", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, webURL, func(out *bytes.Buffer) error {
			if _, err := a.installServer(r.Context(), out); err != nil {
				return err
			}
			openBrowser(webURL) // the web app opens in the person's own browser
			return nil
		})
	})
	mux.HandleFunc("/api/open", func(w http.ResponseWriter, _ *http.Request) {
		openBrowser(webURL)
		reply(w, true, "", "")
	})
	mux.HandleFunc("/api/computer", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Command string `json:"command"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in)
		url, token, ok := parseInstallCommand(in.Command)
		if !ok || url == "" {
			reply(w, false, "I could not find the address and code in that. Paste the whole command from Wardline.", "")
			return
		}
		run(w, r, "", func(out *bytes.Buffer) error { return a.installAgent(r.Context(), out, url, token) })
	})
	mux.HandleFunc("/api/remove", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, "", func(out *bytes.Buffer) error { return a.remove(r.Context(), out) })
	})
	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, true, "", "")
		quit()
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only this computer's own names, so a web page on the internet cannot reach it by pointing a name at 127.0.0.1.
		if host, _, err := net.SplitHostPort(r.Host); err != nil || (host != "127.0.0.1" && host != "localhost") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			if !sameKey(r.URL.Query().Get("k"), key) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(installerPage)
			return
		}
		if r.Method != http.MethodPost || !sameKey(r.Header.Get("X-Wardline-Key"), key) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func sameKey(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
