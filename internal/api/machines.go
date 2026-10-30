package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/store"
)

// Wardline has no people, only machines. Older versions grouped devices under people, so the tables
// still have one. Machines made without one hang off a single hidden entry, which no screen shows.
const hiddenPersonName = "Machines"

// personFor returns the person a new machine belongs to, and the name to build its default name from.
// A caller that still passes a person (older ones do) keeps the old naming, such as "Sam's iPhone".
func (s *Server) personFor(ctx context.Context, tx *sql.Tx, personID, networkID string) (id, name string, err error) {
	if personID != "" {
		if err := tx.QueryRowContext(ctx, `SELECT display_name FROM people WHERE id=?`, personID).Scan(&name); err != nil {
			return "", "", fmt.Errorf("Choose whose device this is.")
		}
		return personID, name, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM people WHERE network_id=? AND display_name=? ORDER BY rowid LIMIT 1`, networkID, hiddenPersonName).Scan(&id)
	if err == nil {
		return id, "", nil
	}
	id = store.NewID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO people(id,network_id,display_name,created_at) VALUES(?,?,?,?)`, id, networkID, hiddenPersonName, s.now().Format("2006-01-02T15:04:05Z07:00")); err != nil {
		return "", "", err
	}
	return id, "", nil
}

// machineName is a machine's name when nobody chose one: its kind, such as "Windows computer".
func machineName(personName, label string) string {
	if personName == "" {
		return label
	}
	return fmt.Sprintf("%s's %s", personName, label)
}

// handleDeviceRename changes a machine's name. Names are unique, so Home and Activity are never ambiguous.
func (s *Server) handleDeviceRename(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 60 {
		s.fail(w, http.StatusBadRequest, "Enter a name for this machine, up to 60 characters.")
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	email := s.adminEmail(ctx, adminID)
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	var old, networkID, oldShort string
	var custom bool
	if err := tx.QueryRowContext(ctx, `SELECT name, network_id, COALESCE(short_name,''), short_name_custom FROM devices WHERE id=? AND status <> 'removed'`, id).Scan(&old, &networkID, &oldShort, &custom); err != nil {
		s.fail(w, http.StatusNotFound, "That machine was not found. It may have been removed.")
		return
	}
	var clash int
	tx.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE network_id=? AND status <> 'removed' AND id <> ? AND name = ? COLLATE NOCASE`, networkID, id, name).Scan(&clash)
	if clash > 0 {
		s.fail(w, http.StatusBadRequest, "Another machine already has that name. Choose a different one.")
		return
	}
	if old != name {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET name=? WHERE id=?`, name, id); err != nil {
			s.serverError(w, r, err)
			return
		}
		sentence := audit.SentenceDeviceRenamed(old, name)
		if !custom {
			short, err := autoShortName(ctx, tx, networkID, id, name)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			if _, err := tx.ExecContext(ctx, `UPDATE devices SET short_name=? WHERE id=?`, short, id); err != nil {
				s.serverError(w, r, err)
				return
			}
			if short != oldShort {
				sentence = audit.SentenceDeviceRenamedOnNetwork(old, name, short)
			}
		}
		if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "device.renamed", SubjectType: "device", SubjectID: id,
			PlainText: sentence}); err != nil {
			s.serverError(w, r, err)
			return
		}
		if err := bumpVersion(ctx, tx); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	ds, err := s.loadDevices(ctx, "AND d.id = ?", id)
	if err != nil || len(ds) == 0 {
		s.serverError(w, r, err)
		return
	}
	s.hub.broadcast() // the server updates its names at once
	s.json(w, http.StatusOK, s.view(ds[0]))
}
