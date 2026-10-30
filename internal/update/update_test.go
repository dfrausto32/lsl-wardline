package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

// fakeGitHub serves one release the way GitHub does, signed with key.
type fakeGitHub struct {
	srv       *httptest.Server
	tag       string
	published time.Time
	tarball   []byte
	sums      string // what SHA256SUMS.txt says; "" means correct for tarball
	sigKey    ed25519.PrivateKey
	draft     bool
	prerel    bool
	noSig     bool
}

func newFake(t *testing.T, key ed25519.PrivateKey) *fakeGitHub {
	f := &fakeGitHub{tag: "v0.2.0", published: now.Add(-3 * time.Hour), tarball: []byte("pretend tarball"), sigKey: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		assets := []map[string]string{
			{"name": "wardline-linux-install.tar.gz", "browser_download_url": f.srv.URL + "/dl/wardline-linux-install.tar.gz"},
			{"name": "SHA256SUMS.txt", "browser_download_url": f.srv.URL + "/dl/SHA256SUMS.txt"},
		}
		if !f.noSig {
			assets = append(assets, map[string]string{"name": "SHA256SUMS.txt.sig", "browser_download_url": f.srv.URL + "/dl/SHA256SUMS.txt.sig"})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "published_at": f.published, "draft": f.draft, "prerelease": f.prerel, "assets": assets})
	})
	mux.HandleFunc("/dl/wardline-linux-install.tar.gz", func(w http.ResponseWriter, r *http.Request) { w.Write(f.tarball) })
	mux.HandleFunc("/dl/SHA256SUMS.txt", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(f.sumsText())) })
	mux.HandleFunc("/dl/SHA256SUMS.txt.sig", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(Sign(f.sigKey, []byte(f.sumsText())))) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) sumsText() string {
	if f.sums != "" {
		return f.sums
	}
	h := sha256.Sum256(f.tarball)
	return hex.EncodeToString(h[:]) + "  wardline-linux-install.tar.gz\n"
}

func cfg(f *fakeGitHub, pub ed25519.PublicKey, current string) *Config {
	return &Config{Repo: "o/r", APIBase: f.srv.URL, PublicKey: pub, Current: current, MinAge: time.Hour, Now: func() time.Time { return now }}
}

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestOnlyNewerVersionsCount(t *testing.T) {
	for _, c := range []struct {
		cur, tag string
		want     bool
	}{
		{"v0.1.11", "v0.1.12", true},
		{"v0.1.11", "v0.1.11", false},
		{"v0.1.11", "v0.1.10", false}, // never go backwards
		{"v0.1.9", "v0.1.10", true},   // numbers, not text
		{"v0.9.0", "v1.0.0", true},
		{"dev", "v0.1.12", false}, // a build that is not a release is never replaced
		{"v0.1.11-3-gabc-dirty", "v0.1.12", false},
		{"v0.1.11", "nonsense", false},
	} {
		if got := Newer(c.cur, c.tag); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.tag, got, c.want)
		}
	}
}

func TestAGoodSignedReleaseIsFetchedAndChecked(t *testing.T) {
	pub, priv := keys(t)
	f := newFake(t, priv)
	c := cfg(f, pub, "v0.1.11")
	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path, err := c.Download(context.Background(), rel, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "pretend tarball" || filepath.Base(path) != "wardline-linux-install.tar.gz" {
		t.Fatalf("wrong file %s %q", path, b)
	}
}

func TestNothingNewWhenAlreadyCurrent(t *testing.T) {
	pub, priv := keys(t)
	f := newFake(t, priv)
	if _, err := cfg(f, pub, "v0.2.0").Check(context.Background()); !errors.Is(err, ErrNothingNew) {
		t.Fatalf("got %v", err)
	}
}

func TestAFreshReleaseWaits(t *testing.T) {
	pub, priv := keys(t)
	f := newFake(t, priv)
	f.published = now.Add(-10 * time.Minute)
	if _, err := cfg(f, pub, "v0.1.11").Check(context.Background()); !errors.Is(err, ErrTooFresh) {
		t.Fatalf("got %v", err)
	}
}

func TestDraftsAndPrereleasesAreIgnored(t *testing.T) {
	pub, priv := keys(t)
	for _, set := range []func(*fakeGitHub){func(f *fakeGitHub) { f.draft = true }, func(f *fakeGitHub) { f.prerel = true }} {
		f := newFake(t, priv)
		set(f)
		if _, err := cfg(f, pub, "v0.1.11").Check(context.Background()); !errors.Is(err, ErrNothingNew) {
			t.Fatalf("got %v", err)
		}
	}
}

func TestASignatureFromAnotherKeyIsRefused(t *testing.T) {
	pub, _ := keys(t)
	_, other := keys(t)
	f := newFake(t, other) // an attacker who controls the release but not your key
	c := cfg(f, pub, "v0.1.11")
	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Download(context.Background(), rel, t.TempDir()); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v", err)
	}
}

func TestAMissingSignatureIsRefused(t *testing.T) {
	pub, priv := keys(t)
	f := newFake(t, priv)
	f.noSig = true
	c := cfg(f, pub, "v0.1.11")
	rel, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Download(context.Background(), rel, t.TempDir()); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v", err)
	}
}

func TestATamperedTarballIsRefusedEvenWithAValidSignature(t *testing.T) {
	pub, priv := keys(t)
	f := newFake(t, priv)
	f.sums = strings.Repeat("0", 64) + "  wardline-linux-install.tar.gz\n" // correctly signed, but not this file
	c := cfg(f, pub, "v0.1.11")
	rel, _ := c.Check(context.Background())
	dir := t.TempDir()
	if _, err := c.Download(context.Background(), rel, dir); !errors.Is(err, ErrBadChecksum) {
		t.Fatalf("got %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("a refused download was left behind: %v", left)
	}
}

func TestPublicKeyParsing(t *testing.T) {
	pub, _ := keys(t)
	got, err := ParsePublicKey(" " + EncodePublicKey(pub) + "\n")
	if err != nil || !pub.Equal(got) {
		t.Fatalf("round trip failed: %v", err)
	}
	for _, bad := range []string{"", "not base64!!", "aGVsbG8="} {
		if _, err := ParsePublicKey(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
