package main

import (
	"os"
	"path/filepath"
	"testing"

	"lsl-wardline/internal/update"
)

func TestSigningRoundTrip(t *testing.T) {
	dir := t.TempDir()
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	defer devnull.Close()
	if err := keygen(dir, devnull); err != nil {
		t.Fatal(err)
	}
	if err := keygen(dir, devnull); err == nil {
		t.Fatal("a second keygen must not overwrite the signing key")
	}
	file := filepath.Join(dir, "SHA256SUMS.txt")
	os.WriteFile(file, []byte("abc  wardline.exe\n"), 0o644)
	if err := signFile(filepath.Join(dir, "release.key"), file); err != nil {
		t.Fatal(err)
	}
	pubText, _ := os.ReadFile(filepath.Join(dir, "release.pub"))
	pub, err := update.ParsePublicKey(string(pubText))
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := os.ReadFile(file + ".sig")
	data, _ := os.ReadFile(file)
	if !update.Verify(pub, data, string(sig)) {
		t.Fatal("the signature did not verify")
	}
	if update.Verify(pub, append(data, 'x'), string(sig)) {
		t.Fatal("a changed file still verified")
	}
}
