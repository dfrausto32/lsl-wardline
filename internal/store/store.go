// Package store owns the SQLite database and its embedded migrations.
package store

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection keeps SQLite simple and predictable.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// DB returns the underlying handle.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE name=?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(name, applied_at) VALUES(?,?)`, name, Now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Now returns the current UTC time as RFC 3339 text, the format used everywhere.
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	idMu   sync.Mutex
	lastMs uint64
	lastR  [10]byte
)

// NewID returns a ULID: 48-bit millisecond time plus 80 bits of randomness,
// monotonic within one process so IDs sort in creation order.
func NewID() string {
	idMu.Lock()
	defer idMu.Unlock()
	ms := uint64(time.Now().UnixMilli())
	if ms <= lastMs {
		ms = lastMs
		for i := 9; i >= 0; i-- {
			lastR[i]++
			if lastR[i] != 0 {
				break
			}
		}
	} else {
		lastMs = ms
		if _, err := rand.Read(lastR[:]); err != nil {
			panic(err)
		}
	}
	var b [16]byte
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (8 * (5 - i)))
	}
	copy(b[6:], lastR[:])
	return encode(b)
}

// encode writes 128 bits as 26 Crockford base32 characters (2 leading pad bits).
func encode(b [16]byte) string {
	out := make([]byte, 0, 26)
	var acc uint32
	bits := uint(2)
	for _, by := range b {
		acc = acc<<8 | uint32(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, crockford[(acc>>bits)&31])
		}
	}
	return string(out)
}
