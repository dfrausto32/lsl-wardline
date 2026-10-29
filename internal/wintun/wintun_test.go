package wintun

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SHA-256 of amd64/wintun.dll from wintun-0.14.1.zip as published at wintun.net/builds.
// The licence forbids modifying the file, so a change here is a mistake, not an update.
const pinnedAMD64 = "e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce"

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestBundledDriverIsTheUnmodifiedOfficialFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("amd64", DLLName))
	if err != nil {
		t.Fatal(err)
	}
	if got := sum(b); got != pinnedAMD64 {
		t.Fatalf("wintun.dll hash is %s, want %s; the official file must not be changed", got, pinnedAMD64)
	}
	if _, err := os.Stat("LICENSE.txt"); err != nil {
		t.Fatal("the Wintun licence must ship next to the driver")
	}
}

func TestEnsureWritesAMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir, []byte("driver")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, DLLName))
	if err != nil || string(got) != "driver" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestEnsureRepairsAChangedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DLLName)
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, []byte("driver")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "driver" {
		t.Fatalf("file was not restored: %q", got)
	}
}

func TestEnsureLeavesAMatchingFileAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DLLName)
	if err := os.WriteFile(path, []byte("driver"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, []byte("driver")); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if !st.ModTime().Equal(old) {
		t.Fatal("an identical file was rewritten")
	}
}
