package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/store"
)

func seed(t *testing.T) (dir string, st *store.Store) {
	t.Helper()
	dir = t.TempDir()
	st, err := store.Open(filepath.Join(dir, "wardline.db"))
	if err != nil {
		t.Fatal(err)
	}
	l := audit.New(st.DB())
	for i := 0; i < 3; i++ {
		if err := l.Append(context.Background(), st.DB(), audit.Event{Actor: "Maria", Action: "x", SubjectType: "device", SubjectID: "d", PlainText: "Maria added a device."}); err != nil {
			t.Fatal(err)
		}
	}
	return dir, st
}

func runCLI(args ...string) (string, error) {
	var out bytes.Buffer
	root := newRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestAuditVerifyCommandPassesOnUntouchedLogAndFailsOnEdits(t *testing.T) {
	dir, st := seed(t)
	out, err := runCLI("audit", "verify", "--data", dir)
	if err != nil || !strings.Contains(out, "3 entries") {
		t.Fatalf("untouched: %q %v", out, err)
	}

	// Someone removes the protection and edits a row.
	st.DB().Exec(`DROP TRIGGER audit_events_no_update`)
	st.DB().Exec(`UPDATE audit_events SET plain_text='Nothing happened' WHERE rowid=2`)
	st.Close()
	if _, err := runCLI("audit", "verify", "--data", dir); err == nil || !strings.Contains(err.Error(), "row 2") {
		t.Fatalf("edited log must fail verification, got %v", err)
	}
}

func TestAuditExportWritesCSV(t *testing.T) {
	dir, st := seed(t)
	st.Close()
	out, err := runCLI("audit", "export", "--data", dir)
	if err != nil || !strings.HasPrefix(out, "When,Who,What happened") || strings.Count(out, "\n") != 4 {
		t.Fatalf("%q %v", out, err)
	}
}

func TestAuditVerifyWithoutADatabaseSaysSo(t *testing.T) {
	if _, err := runCLI("audit", "verify", "--data", t.TempDir()); err == nil || !strings.Contains(err.Error(), "no database") {
		t.Fatalf("got %v", err)
	}
}
