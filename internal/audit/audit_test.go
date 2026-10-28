package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"lsl-wardline/internal/store"
)

const fakeKey = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="

func setup(t *testing.T) (*Log, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s.DB()), s
}

func ev(text string) Event {
	return Event{Actor: "maria@example.com", Action: "device.added", SubjectType: "device", SubjectID: "d1", PlainText: text}
}

func appendN(t *testing.T, l *Log, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := l.Append(context.Background(), l.db, ev("Maria added a device")); err != nil {
			t.Fatal(err)
		}
	}
}

// dropTriggers lets a test simulate someone editing the file directly.
func dropTriggers(t *testing.T, s *store.Store) {
	t.Helper()
	for _, q := range []string{`DROP TRIGGER audit_events_no_update`, `DROP TRIGGER audit_events_no_delete`} {
		if _, err := s.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyEmptyAndUntouchedLog(t *testing.T) {
	l, _ := setup(t)
	if n, err := l.Verify(context.Background()); err != nil || n != 0 {
		t.Fatalf("empty: n=%d err=%v", n, err)
	}
	appendN(t, l, 5)
	n, err := l.Verify(context.Background())
	if err != nil || n != 5 {
		t.Fatalf("untouched: n=%d err=%v", n, err)
	}
}

func TestChainLinksEachRowToThePrevious(t *testing.T) {
	l, s := setup(t)
	appendN(t, l, 3)
	rows, _ := s.DB().Query(`SELECT prev_hash, hash FROM audit_events ORDER BY rowid`)
	defer rows.Close()
	prev := strings.Repeat("0", 64)
	for rows.Next() {
		var p, h string
		rows.Scan(&p, &h)
		if p != prev {
			t.Fatalf("prev_hash %s want %s", p, prev)
		}
		if len(h) != 64 || h == p {
			t.Fatalf("bad hash %q", h)
		}
		prev = h
	}
}

func TestVerifyFailsWhenARowIsAltered(t *testing.T) {
	l, s := setup(t)
	appendN(t, l, 4)
	dropTriggers(t, s)
	if _, err := s.DB().Exec(`UPDATE audit_events SET plain_text='Nobody did anything' WHERE rowid=2`); err != nil {
		t.Fatal(err)
	}
	_, err := l.Verify(context.Background())
	var be *BrokenError
	if !errors.As(err, &be) {
		t.Fatalf("want BrokenError, got %v", err)
	}
	if be.Row != 2 {
		t.Fatalf("broken row = %d, want 2", be.Row)
	}
}

func TestVerifyFailsWhenARowIsDeleted(t *testing.T) {
	l, s := setup(t)
	appendN(t, l, 4)
	dropTriggers(t, s)
	s.DB().Exec(`DELETE FROM audit_events WHERE rowid=2`)
	if _, err := l.Verify(context.Background()); err == nil {
		t.Fatal("deleting a row must break verification")
	}
}

func TestVerifyFailsWhenHashIsRecomputedButChainBreaks(t *testing.T) {
	l, s := setup(t)
	appendN(t, l, 3)
	dropTriggers(t, s)
	// An attacker rewrites row 1 and fixes its own hash but cannot fix row 2's
	// prev_hash without rewriting everything after it.
	var id, ts, actor, action, st, sid, details, prev string
	s.DB().QueryRow(`SELECT id, ts, actor, action, subject_type, subject_id, details, prev_hash FROM audit_events WHERE rowid=1`).
		Scan(&id, &ts, &actor, &action, &st, &sid, &details, &prev)
	h := computeHash(prev, id, ts, actor, action, st, sid, "Forged sentence", details)
	s.DB().Exec(`UPDATE audit_events SET plain_text='Forged sentence', hash=? WHERE rowid=1`, h)
	if _, err := l.Verify(context.Background()); err == nil {
		t.Fatal("forged row 1 must be caught at row 2")
	}
}

func TestUpdateAndDeleteAreBlockedByTheDatabase(t *testing.T) {
	l, s := setup(t)
	appendN(t, l, 1)
	if _, err := s.DB().Exec(`UPDATE audit_events SET plain_text='x'`); err == nil {
		t.Fatal("update should be blocked")
	}
	if _, err := s.DB().Exec(`DELETE FROM audit_events`); err == nil {
		t.Fatal("delete should be blocked")
	}
}

func TestAppendRedactsSecretsInTextAndDetails(t *testing.T) {
	l, s := setup(t)
	e := ev("Maria added a device " + fakeKey)
	e.Details = map[string]any{"public_key": fakeKey, "private_key": fakeKey, "note": "see " + fakeKey, "count": 2}
	if err := l.Append(context.Background(), l.db, e); err != nil {
		t.Fatal(err)
	}
	var text, details string
	s.DB().QueryRow(`SELECT plain_text, details FROM audit_events`).Scan(&text, &details)
	if strings.Contains(text, fakeKey) {
		t.Fatalf("text leaked: %s", text)
	}
	if strings.Count(details, fakeKey) != 1 {
		t.Fatalf("only the public key may be kept: %s", details)
	}
	if _, err := l.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAppendInsideCallerTransactionRollsBackWithIt(t *testing.T) {
	l, s := setup(t)
	tx, _ := s.DB().Begin()
	if err := l.Append(context.Background(), tx, ev("x")); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	var n int
	s.DB().QueryRow(`SELECT count(*) FROM audit_events`).Scan(&n)
	if n != 0 {
		t.Fatalf("rolled back event persisted: %d", n)
	}
}

func TestAppendRejectsIncompleteEvents(t *testing.T) {
	l, _ := setup(t)
	if err := l.Append(context.Background(), l.db, Event{Action: "x"}); err == nil {
		t.Fatal("missing plain text / actor should be rejected")
	}
}

func TestListFilterAndCSV(t *testing.T) {
	l, _ := setup(t)
	ctx := context.Background()
	a := Event{Actor: "Maria", Action: "device.added", SubjectType: "device", SubjectID: "d1", PlainText: "Maria added Sam's iPhone.", Details: map[string]any{"person_id": "p1"}}
	b := Event{Actor: "Maria", Action: "device.removed", SubjectType: "device", SubjectID: "d2", PlainText: "Alex's laptop was removed.", Details: map[string]any{"person_id": "p2"}}
	l.Append(ctx, l.db, a)
	l.Append(ctx, l.db, b)

	all, err := l.List(ctx, Filter{})
	if err != nil || len(all) != 2 {
		t.Fatalf("all: %v %v", len(all), err)
	}
	if all[0].PlainText != b.PlainText {
		t.Fatal("newest first expected")
	}
	byPerson, _ := l.List(ctx, Filter{PersonID: "p1"})
	if len(byPerson) != 1 || byPerson[0].SubjectID != "d1" {
		t.Fatalf("person filter: %+v", byPerson)
	}
	byDevice, _ := l.List(ctx, Filter{SubjectID: "d2"})
	if len(byDevice) != 1 {
		t.Fatalf("subject filter: %+v", byDevice)
	}

	var buf bytes.Buffer
	if err := l.ExportCSV(ctx, &buf, Filter{}); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(recs) != 3 {
		t.Fatalf("csv: %v rows=%d", err, len(recs))
	}
	if recs[0][0] != "When" || recs[0][1] != "Who" || recs[0][2] != "What happened" {
		t.Fatalf("header: %v", recs[0])
	}
}

func TestCSVDefendsAgainstFormulaInjection(t *testing.T) {
	l, _ := setup(t)
	l.Append(context.Background(), l.db, ev(`=HYPERLINK("http://evil")`))
	var buf bytes.Buffer
	l.ExportCSV(context.Background(), &buf, Filter{})
	recs, _ := csv.NewReader(&buf).ReadAll()
	if strings.HasPrefix(recs[1][2], "=") {
		t.Fatalf("cell starts with =: %q", recs[1][2])
	}
}

func TestSentencesAreWordedForPeopleNotNetworkers(t *testing.T) {
	sentences := []string{
		SentenceNetworkCreated("Maria", "Home"),
		SentenceDeviceAdded("Maria", "Sam's iPhone"),
		SentenceDeviceRemoved("Maria", "Sam's laptop"),
		SentenceServerCheckedIn("Server 1"),
		SentenceAdminSignedIn("Maria"),
		SentenceDeviceSetUp("Sam's laptop"),
		SentenceDeviceRefreshed("Sam's laptop"),
		SentenceDeviceRefreshFailed("Sam's laptop"),
	}
	banned := []string{"peer", "node", "client", "gateway", "hub", "endpoint", "vpn", "tunnel", "handshake", "revoke", "rotate", "migrate", "audit log", "public key", "private key"}
	for _, s := range sentences {
		if !strings.HasSuffix(s, ".") {
			t.Errorf("sentence should end with a full stop: %q", s)
		}
		low := strings.ToLower(s)
		for _, b := range banned {
			if strings.Contains(low, b) {
				t.Errorf("sentence %q contains banned word %q", s, b)
			}
		}
	}
	if got := SentenceDeviceRemoved("Maria", "Sam's laptop"); got != "Sam's laptop was removed." {
		t.Errorf("got %q", got)
	}
	if got := SentenceDeviceAdded("Maria", "Sam's iPhone"); got != "Maria added Sam's iPhone." {
		t.Errorf("got %q", got)
	}
}
