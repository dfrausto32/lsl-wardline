package membership

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAFileSignerMakesItsKeyOnceAndKeepsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "authority.key")
	a, err := NewFileSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewFileSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Public().Equal(b.Public()) {
		t.Fatal("opening the same file must give the same key")
	}
	sig, err := a.Sign([]byte("hello"))
	if err != nil || !ed25519.Verify(b.Public(), []byte("hello"), sig) {
		t.Fatalf("a signature from one opening must verify with the other: %v", err)
	}
}

func TestTheKeyFileIsPrivateToItsOwner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows; the Windows install locks the folder instead")
	}
	path := filepath.Join(t.TempDir(), "authority.key")
	if _, err := NewFileSigner(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("the key file is %v, want 0600", st.Mode().Perm())
	}
	// A key file anyone can read is refused rather than used.
	os.Chmod(path, 0o644)
	if _, err := NewFileSigner(path); err == nil || !strings.Contains(err.Error(), "readable") {
		t.Fatalf("a world-readable key file must be refused: %v", err)
	}
}

func TestADamagedKeyFileIsRefusedAndNeverReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.key")
	os.WriteFile(path, []byte("not a key"), 0o600)
	if _, err := NewFileSigner(path); err == nil {
		t.Fatal("expected an error")
	}
	if b, _ := os.ReadFile(path); string(b) != "not a key" {
		t.Fatal("a damaged key file must not be overwritten: that would silently change who the authority is")
	}
}

func TestTheKeyFileHoldsNoMoreThanTheSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.key")
	s, err := NewFileSigner(path)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if len(strings.TrimSpace(string(b))) != 44 { // base64 of 32 bytes
		t.Fatalf("unexpected key file contents (%d characters)", len(b))
	}
	_ = s
}
