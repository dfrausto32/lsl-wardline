// Package audit is the only way Wardline records what happened. Every change
// goes through Append. Rows are hash-chained and shown to people as "Activity".
package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	wlog "lsl-wardline/internal/log"
	"lsl-wardline/internal/store"
)

// Genesis is the prev_hash of the first row.
var Genesis = strings.Repeat("0", 64)

// DBTX is satisfied by *sql.DB and *sql.Tx, so an event can be written in the
// same transaction as the change it describes.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Event is what a caller supplies. PlainText is the sentence shown to people.
type Event struct {
	Actor       string
	Action      string
	SubjectType string
	SubjectID   string
	PlainText   string
	Details     map[string]any
}

// Record is a stored event.
type Record struct {
	ID          string
	TS          string
	Actor       string
	Action      string
	SubjectType string
	SubjectID   string
	PlainText   string
	Details     string
}

// Filter narrows List and ExportCSV. Zero value matches everything.
type Filter struct {
	SubjectID string // a device, person, or server
	PersonID  string // events about this person or their devices
	Limit     int
}

// BrokenError says where the chain stopped matching.
type BrokenError struct {
	Row    int64
	Reason string
}

func (e *BrokenError) Error() string {
	return fmt.Sprintf("activity log is broken at row %d: %s", e.Row, e.Reason)
}

// Log writes and reads audit events.
type Log struct {
	db *sql.DB
}

func New(db *sql.DB) *Log { return &Log{db: db} }

func computeHash(prev, id, ts, actor, action, subjectType, subjectID, plain, details string) string {
	h := sha256.New()
	for _, part := range []string{prev, id, ts, actor, action, subjectType, subjectID, plain, details} {
		fmt.Fprintf(h, "%d:%s|", len(part), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func cleanDetails(in map[string]any) (string, error) {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch {
		case wlog.IsSensitiveName(k):
			out[k] = "[redacted]"
		default:
			if s, ok := v.(string); ok {
				if k == "public_key" || strings.HasSuffix(k, "_public_key") {
					out[k] = s
				} else {
					out[k] = wlog.Redact(s)
				}
				continue
			}
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			var back any
			if err := json.Unmarshal([]byte(wlog.Redact(string(b))), &back); err != nil {
				return "", err
			}
			out[k] = back
		}
	}
	b, err := json.Marshal(out) // map keys are sorted, so this is stable
	return string(b), err
}

// Append adds one event to the chain using q (a *sql.DB or the caller's *sql.Tx).
func (l *Log) Append(ctx context.Context, q DBTX, e Event) error {
	if e.Actor == "" || e.Action == "" || e.SubjectType == "" || e.PlainText == "" {
		return fmt.Errorf("audit: actor, action, subject type and plain text are required")
	}
	details, err := cleanDetails(e.Details)
	if err != nil {
		return err
	}
	plain := wlog.Redact(e.PlainText)
	prev := Genesis
	err = q.QueryRowContext(ctx, `SELECT hash FROM audit_events ORDER BY rowid DESC LIMIT 1`).Scan(&prev)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	id := store.NewID()
	ts := time.Now().UTC().Format(time.RFC3339)
	hash := computeHash(prev, id, ts, e.Actor, e.Action, e.SubjectType, e.SubjectID, plain, details)
	_, err = q.ExecContext(ctx, `INSERT INTO audit_events(id,ts,actor,action,subject_type,subject_id,plain_text,details,prev_hash,hash)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, id, ts, e.Actor, e.Action, e.SubjectType, e.SubjectID, plain, details, prev, hash)
	return err
}

// Verify recomputes the whole chain. It returns the number of rows checked, or
// a *BrokenError naming the first row that does not match.
func (l *Log) Verify(ctx context.Context) (int, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT rowid, id, ts, actor, action, subject_type, subject_id, plain_text, details, prev_hash, hash
		FROM audit_events ORDER BY rowid`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	prev := Genesis
	n := 0
	for rows.Next() {
		var rowid int64
		var id, ts, actor, action, st, sid, plain, details, p, h string
		if err := rows.Scan(&rowid, &id, &ts, &actor, &action, &st, &sid, &plain, &details, &p, &h); err != nil {
			return n, err
		}
		if p != prev {
			return n, &BrokenError{Row: rowid, Reason: "it does not follow the row before it"}
		}
		if computeHash(p, id, ts, actor, action, st, sid, plain, details) != h {
			return n, &BrokenError{Row: rowid, Reason: "its contents were changed"}
		}
		prev = h
		n++
	}
	return n, rows.Err()
}

func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	if f.SubjectID != "" {
		conds = append(conds, "subject_id = ?")
		args = append(args, f.SubjectID)
	}
	if f.PersonID != "" {
		conds = append(conds, "(subject_id = ? OR json_extract(details, '$.person_id') = ?)")
		args = append(args, f.PersonID, f.PersonID)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// List returns events newest first.
func (l *Log) List(ctx context.Context, f Filter) ([]Record, error) {
	where, args := f.where()
	q := `SELECT id, ts, actor, action, subject_type, subject_id, plain_text, details FROM audit_events` + where + ` ORDER BY rowid DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.TS, &r.Actor, &r.Action, &r.SubjectType, &r.SubjectID, &r.PlainText, &r.Details); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// ExportCSV writes the Activity timeline as CSV, oldest first.
func (l *Log) ExportCSV(ctx context.Context, w io.Writer, f Filter) error {
	recs, err := l.List(ctx, f)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"When", "Who", "What happened"}); err != nil {
		return err
	}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if err := cw.Write([]string{r.TS, csvSafe(r.Actor), csvSafe(r.PlainText)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
