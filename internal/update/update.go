// Package update finds, downloads and checks new Wardline releases for a server that updates itself.
// A release is installed only if SHA256SUMS.txt carries a signature from the owner's key, so someone who
// takes over the GitHub repository still cannot make a server run their code.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	tarballName = "wardline-linux-install.tar.gz"
	sumsName    = "SHA256SUMS.txt"
	sigName     = "SHA256SUMS.txt.sig"
)

var (
	ErrNothingNew   = errors.New("there is no newer release")
	ErrTooFresh     = errors.New("the newest release is too new to install yet")
	ErrBadSignature = errors.New("the release is not signed by the expected key")
	ErrBadChecksum  = errors.New("the downloaded file does not match its checksum")
)

type Config struct {
	Repo      string // owner/name
	APIBase   string // https://api.github.com unless testing
	PublicKey ed25519.PublicKey
	Current   string // the version running now
	MinAge    time.Duration
	Now       func() time.Time
	Client    *http.Client
}

type Release struct {
	Tag       string
	Published time.Time
	assets    map[string]string
}

func (c *Config) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (c *Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Config) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "wardline-update")
	res, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", url)
	}
	return b, nil
}

// Check looks for a release that is newer than the running version and old enough to trust.
func (c *Config) Check(ctx context.Context) (*Release, error) {
	base := c.APIBase
	if base == "" {
		base = "https://api.github.com"
	}
	b, err := c.get(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", base, c.Repo), 4<<20)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Tag        string    `json:"tag_name"`
		Published  time.Time `json:"published_at"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.Draft || raw.Prerelease || !Newer(c.Current, raw.Tag) {
		return nil, ErrNothingNew
	}
	if c.now().Sub(raw.Published) < c.MinAge {
		return nil, ErrTooFresh
	}
	rel := &Release{Tag: raw.Tag, Published: raw.Published, assets: map[string]string{}}
	for _, a := range raw.Assets {
		rel.assets[a.Name] = a.URL
	}
	return rel, nil
}

// Download fetches the Linux install package into dir and returns its path, but only after the
// signature on the checksum list and the package's own checksum both check out. Nothing is left behind otherwise.
func (c *Config) Download(ctx context.Context, rel *Release, dir string) (string, error) {
	for _, n := range []string{tarballName, sumsName, sigName} {
		if rel.assets[n] == "" {
			if n == sigName {
				return "", fmt.Errorf("%w: %s has no signature file", ErrBadSignature, rel.Tag)
			}
			return "", fmt.Errorf("%s has no %s", rel.Tag, n)
		}
	}
	sums, err := c.get(ctx, rel.assets[sumsName], 1<<20)
	if err != nil {
		return "", err
	}
	sig, err := c.get(ctx, rel.assets[sigName], 1<<10)
	if err != nil {
		return "", err
	}
	if !Verify(c.PublicKey, sums, string(sig)) {
		return "", ErrBadSignature
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == tarballName {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return "", fmt.Errorf("the checksum list has no entry for %s", tarballName)
	}
	body, err := c.get(ctx, rel.assets[tarballName], 200<<20)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(body)
	if hex.EncodeToString(got[:]) != want {
		return "", ErrBadChecksum
	}
	path := filepath.Join(dir, tarballName)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

var versionRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

func parseVersion(v string) ([3]int, bool) {
	m := versionRE.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out, true
}

// Newer says whether tag is a later release than current. Anything that is not a plain release number,
// such as a development build, is never replaced and never offered.
func Newer(current, tag string) bool {
	a, ok1 := parseVersion(current)
	b, ok2 := parseVersion(tag)
	if !ok1 || !ok2 {
		return false
	}
	for i := range a {
		if b[i] != a[i] {
			return b[i] > a[i]
		}
	}
	return false
}

// ---- keys ----

func EncodePublicKey(k ed25519.PublicKey) string { return base64.StdEncoding.EncodeToString(k) }

func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("that is not a Wardline release public key")
	}
	return ed25519.PublicKey(b), nil
}

// Sign returns the signature of data as text, for the .sig file that goes next to SHA256SUMS.txt.
func Sign(k ed25519.PrivateKey, data []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(k, data))
}

func Verify(k ed25519.PublicKey, data []byte, sig string) bool {
	if len(k) != ed25519.PublicKeySize {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	return err == nil && len(b) == ed25519.SignatureSize && ed25519.Verify(k, data, b)
}
