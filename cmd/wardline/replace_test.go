package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeContent(content string, lockedWhileOldIsThere bool) func(string) error {
	return func(path string) error {
		if _, err := os.Stat(path); err == nil && lockedWhileOldIsThere {
			return errors.New("The process cannot access the file because it is being used by another process.")
		}
		return os.WriteFile(path, []byte(content), 0o755)
	}
}

func TestReplacingAFreeFileJustWritesIt(t *testing.T) {
	to := filepath.Join(t.TempDir(), "wardline.exe")
	os.WriteFile(to, []byte("old"), 0o755)
	if err := replaceFile(to, writeContent("new", false)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(to); string(b) != "new" {
		t.Fatalf("got %q", b)
	}
	if _, err := os.Stat(to + ".old"); err == nil {
		t.Fatal("nothing needed moving aside")
	}
}

// Windows will not let a running program be overwritten, but it does let it be renamed.
func TestAProgramThatIsInUseIsMovedAsideAndReplaced(t *testing.T) {
	to := filepath.Join(t.TempDir(), "wardline.exe")
	os.WriteFile(to, []byte("old"), 0o755)
	if err := replaceFile(to, writeContent("new", true)); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(to); string(b) != "new" {
		t.Fatalf("the new file was not written: %q", b)
	}
	if b, _ := os.ReadFile(to + ".old"); string(b) != "old" {
		t.Fatalf("the old file should be kept aside: %q", b)
	}
}

func TestReplacingTwiceInUseKeepsWorking(t *testing.T) {
	to := filepath.Join(t.TempDir(), "wardline.exe")
	os.WriteFile(to, []byte("v1"), 0o755)
	for _, v := range []string{"v2", "v3"} {
		if err := replaceFile(to, writeContent(v, true)); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	if b, _ := os.ReadFile(to); string(b) != "v3" {
		t.Fatalf("got %q", b)
	}
}

func TestAnImpossibleReplaceIsExplainedPlainly(t *testing.T) {
	to := filepath.Join(t.TempDir(), "nodir", "wardline.exe") // the folder does not exist, so nothing can work
	err := replaceFile(to, writeContent("new", false))
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestLeftoverOldFilesAreCleanedUp(t *testing.T) {
	dir := t.TempDir()
	to := filepath.Join(dir, "wardline.exe")
	os.WriteFile(to+".old", []byte("x"), 0o755)
	removeAside(to)
	if _, err := os.Stat(to + ".old"); err == nil {
		t.Fatal("the leftover was not removed")
	}
}
