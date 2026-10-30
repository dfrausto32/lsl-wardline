package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/update"
)

func runUpdate(t *testing.T, args ...string) (string, error) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"update"}, args...))
	err := root.Execute()
	return out.String(), err
}

func fakeRelease(t *testing.T, tag string, age time.Duration) (apiBase, keyFile string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "published_at": time.Now().Add(-age), "assets": []any{}})
	}))
	t.Cleanup(srv.Close)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	keyFile = filepath.Join(t.TempDir(), "release.pub")
	os.WriteFile(keyFile, []byte(update.EncodePublicKey(pub)+"\n"), 0o600)
	return srv.URL, keyFile
}

func TestUpdateCheckReportsInPlainWords(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "v0.1.0"

	api, key := fakeRelease(t, "v0.1.5", 3*time.Hour)
	out, err := runUpdate(t, "check", "--api-base", api, "--key-file", key, "--repo", "o/r")
	if err != nil || !strings.Contains(out, "v0.1.5 is ready to install") {
		t.Fatalf("%q %v", out, err)
	}

	api, key = fakeRelease(t, "v0.1.5", time.Minute)
	out, err = runUpdate(t, "check", "--api-base", api, "--key-file", key, "--repo", "o/r")
	if err != nil || !strings.Contains(out, "waiting period") {
		t.Fatalf("%q %v", out, err)
	}

	version = "v0.1.5"
	api, key = fakeRelease(t, "v0.1.5", 3*time.Hour)
	out, err = runUpdate(t, "check", "--api-base", api, "--key-file", key, "--repo", "o/r")
	if err != nil || !strings.Contains(out, "Up to date") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestUpdateWithoutAKeyExplainsWhy(t *testing.T) {
	_, err := runUpdate(t, "check", "--key-file", filepath.Join(t.TempDir(), "missing.pub"))
	if err == nil || !strings.Contains(err.Error(), "release public key") {
		t.Fatalf("got %v", err)
	}
}
