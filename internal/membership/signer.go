package membership

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Signer signs log entries without handing out the private key. A file key, a hardware module or a cloud
// key service can each sit behind it.
type Signer interface {
	Public() ed25519.PublicKey
	Sign(msg []byte) ([]byte, error)
}

// KeySigner signs with a key held in memory (the control plane's own key, loaded from its sealed storage).
type KeySigner struct{ Key ed25519.PrivateKey }

func (k KeySigner) Public() ed25519.PublicKey { return k.Key.Public().(ed25519.PublicKey) }

func (k KeySigner) Sign(msg []byte) ([]byte, error) { return ed25519.Sign(k.Key, msg), nil }

// FileSigner keeps its key in a file only its owner can read. The file holds the key's 32-byte seed as base64.
// It is made on first use. Another kind of signer (a hardware module, a cloud key service) can replace it
// without anything else changing.
type FileSigner struct{ KeySigner }

func NewFileSigner(path string) (*FileSigner, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(seed)+"\n"), 0o600); err != nil {
			return nil, err
		}
		return &FileSigner{KeySigner{ed25519.NewKeyFromSeed(seed)}}, nil
	}
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("the key file %s is readable by other users; run chmod 600 on it", path)
		}
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("the key file %s is not a signing key", path)
	}
	return &FileSigner{KeySigner{ed25519.NewKeyFromSeed(seed)}}, nil
}
