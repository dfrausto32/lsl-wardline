package api

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/gateway"
	"lsl-wardline/internal/names"
	"lsl-wardline/internal/store"
)

// Names for machines: specs/BUSINESS-RULES.md, "Names for machines".

const maxExtraNames = 10

type rowsQuerier interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// takenNames are the short names and extra names in use on the network by machines other than except.
func takenNames(ctx context.Context, q rowsQuerier, networkID, except string) (map[string]bool, error) {
	taken := map[string]bool{}
	for _, query := range []string{
		`SELECT short_name FROM devices WHERE network_id=? AND status <> 'removed' AND id <> ? AND short_name IS NOT NULL`,
		`SELECT a.name FROM device_aliases a JOIN devices d ON d.id=a.device_id WHERE a.network_id=? AND d.status <> 'removed' AND a.device_id <> ?`,
	} {
		rows, err := q.QueryContext(ctx, query, networkID, except)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return nil, err
			}
			taken[n] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return taken, nil
}

// autoShortName is the short name a machine's name makes, numbered if another machine already has it.
func autoShortName(ctx context.Context, q rowsQuerier, networkID, deviceID, friendly string) (string, error) {
	taken, err := takenNames(ctx, q, networkID, deviceID)
	if err != nil {
		return "", err
	}
	return names.Assign(names.Slug(friendly), func(s string) bool { return !taken[s] }), nil
}

// giveShortName sets a new machine's short name from its name.
func giveShortName(ctx context.Context, tx *sql.Tx, networkID, deviceID, friendly string) error {
	short, err := autoShortName(ctx, tx, networkID, deviceID, friendly)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE devices SET short_name=?, short_name_custom=0 WHERE id=?`, short, deviceID)
	return err
}

// backfillShortNames gives machines that existed before names a short name from their current name.
func (s *Server) backfillShortNames(ctx context.Context) error {
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, network_id, name FROM devices WHERE status <> 'removed' AND short_name IS NULL ORDER BY added_at, id`)
	if err != nil {
		return err
	}
	type todo struct{ id, network, name string }
	var list []todo
	for rows.Next() {
		var t todo
		if err := rows.Scan(&t.id, &t.network, &t.name); err != nil {
			rows.Close()
			return err
		}
		list = append(list, t)
	}
	rows.Close()
	for _, t := range list {
		if err := giveShortName(ctx, tx, t.network, t.id, t.name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// nameRecords is everything the server's name service should answer, in a fixed order.
func (s *Server) nameRecords(ctx context.Context, serverIP string) ([]gateway.NameRecord, error) {
	out := []gateway.NameRecord{{Name: names.ServerName, IP: serverIP}}
	rows, err := s.d.Store.DB().QueryContext(ctx, `SELECT d.short_name, d.tunnel_ip FROM devices d
		WHERE d.status IN ('waiting','active') AND d.short_name IS NOT NULL
		UNION ALL
		SELECT a.name, d.tunnel_ip FROM device_aliases a JOIN devices d ON d.id=a.device_id WHERE d.status IN ('waiting','active')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r gateway.NameRecord
		if err := rows.Scan(&r.Name, &r.IP); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// handleNamePreview says what name a machine would get, for the line under the name box.
func (s *Server) handleNamePreview(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	n, err := s.network(ctx, s.d.Store.DB())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wanted := names.Slug(strings.TrimSpace(r.URL.Query().Get("name")))
	short, err := autoShortName(ctx, s.d.Store.DB(), n.ID, r.URL.Query().Get("device"), r.URL.Query().Get("name"))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, map[string]any{"short_name": short, "name_on_network": names.Full(short), "wanted": wanted, "numbered": short != wanted})
}

// handleDeviceNames sets a machine's short name by hand (or hands it back to the machine's name) and its extra names.
func (s *Server) handleDeviceNames(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		ShortName string   `json:"short_name"`
		Aliases   []string `json:"aliases"`
	}
	if !s.decode(w, r, &in) {
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
	var friendly, networkID string
	if err := tx.QueryRowContext(ctx, `SELECT name, network_id FROM devices WHERE id=? AND status <> 'removed'`, id).Scan(&friendly, &networkID); err != nil {
		s.fail(w, http.StatusNotFound, "That machine was not found. It may have been removed.")
		return
	}
	taken, err := takenNames(ctx, tx, networkID, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	short, custom := "", 0
	if in.ShortName != "" {
		if err := names.Check(in.ShortName); err != nil {
			s.fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if taken[in.ShortName] {
			s.fail(w, http.StatusBadRequest, "Another machine already uses that name. Choose a different one.")
			return
		}
		short, custom = in.ShortName, 1
	} else {
		short = names.Assign(names.Slug(friendly), func(n string) bool { return !taken[n] })
	}

	if len(in.Aliases) > maxExtraNames {
		s.fail(w, http.StatusBadRequest, "A machine can have up to 10 extra names.")
		return
	}
	seen := map[string]bool{short: true}
	for _, a := range in.Aliases {
		if err := names.Check(a); err != nil {
			s.fail(w, http.StatusBadRequest, "The extra name "+a+" can't be used. "+err.Error())
			return
		}
		if taken[a] || seen[a] {
			s.fail(w, http.StatusBadRequest, "The name "+a+" is already in use. Choose a different one.")
			return
		}
		seen[a] = true
	}

	ts := s.now().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET short_name=?, short_name_custom=? WHERE id=?`, short, custom, id); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_aliases WHERE device_id=?`, id); err != nil {
		s.serverError(w, r, err)
		return
	}
	for _, a := range in.Aliases {
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_aliases(id,device_id,network_id,name,created_at) VALUES(?,?,?,?,?)`, store.NewID(), id, networkID, a, ts); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "device.names_changed", SubjectType: "device", SubjectID: id,
		PlainText: audit.SentenceDeviceNames(email, friendly, short, in.Aliases)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := bumpVersion(ctx, tx); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.hub.broadcast() // the server and the computers hear about it at once
	ds, err := s.loadDevices(ctx, "AND d.id = ?", id)
	if err != nil || len(ds) == 0 {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, s.view(ds[0]))
}
