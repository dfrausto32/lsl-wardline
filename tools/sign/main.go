// Command sign makes and uses the release signing key. It is run by the person who publishes releases,
// on their own computer; the private key never goes into the repository or to GitHub.
//
//	go run ./tools/sign keygen <folder>            makes release.key (private) and release.pub (public) in the folder
//	go run ./tools/sign sign <release.key> <file>  writes <file>.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lsl-wardline/internal/update"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string, out *os.File) error {
	if len(args) == 0 {
		return errors.New("usage: sign keygen <folder> | sign sign <release.key> <file>")
	}
	switch args[0] {
	case "keygen":
		if len(args) != 2 {
			return errors.New("usage: sign keygen <folder>")
		}
		return keygen(args[1], out)
	case "sign":
		if len(args) != 3 {
			return errors.New("usage: sign sign <release.key> <file>")
		}
		return signFile(args[1], args[2])
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func keygen(dir string, out *os.File) error {
	priv := filepath.Join(dir, "release.key")
	if _, err := os.Stat(priv); err == nil {
		return fmt.Errorf("%s already exists; refusing to replace a signing key", priv)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(priv, []byte(base64.StdEncoding.EncodeToString(key.Seed())+"\n"), 0o600); err != nil {
		return err
	}
	text := update.EncodePublicKey(pub)
	if err := os.WriteFile(filepath.Join(dir, "release.pub"), []byte(text+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(out, text)
	return nil
}

func loadKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("that is not a release signing key")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func signFile(keyPath, file string) error {
	key, err := loadKey(keyPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file+".sig", []byte(update.Sign(key, data)+"\n"), 0o644)
}
