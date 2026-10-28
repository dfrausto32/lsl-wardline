package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"lsl-wardline/internal/agent"
	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/store"
	"lsl-wardline/internal/wg"
)

const (
	agentRefreshAge    = 90 * 24 * time.Hour // how often an agent replaces its own key
	refreshRetryAfter  = 24 * time.Hour      // after a failed refresh
	refreshOverlap     = time.Hour           // how long the old key can still be gone back to
	agentReportFresh   = 10 * time.Minute    // an agent's own report is trusted for this long
	deviceFutureLeeway = time.Minute
)

// handleAgentRefresh swaps the device's key for a new one the device made. The old key is kept (retired)
// for an hour so the device can go back if the new one does not connect. WireGuard allows only one key
// per tunnel address, so there is a short switch rather than two keys at once.
func (s *Server) handleAgentRefresh(w http.ResponseWriter, r *http.Request, deviceID string) {
	var in agent.RefreshRequest
	if !s.decode(w, r, &in) {
		return
	}
	if wg.ValidateKey(in.PublicKey) != nil {
		s.fail(w, http.StatusBadRequest, "The device sent an invalid security code.")
		return
	}
	ctx := r.Context()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	var keyID, current, name, personID string
	if err := tx.QueryRowContext(ctx, `SELECT k.id, k.public_key, d.name, d.person_id FROM device_keys k JOIN devices d ON d.id=k.device_id
		WHERE k.device_id=? AND k.state='active'`, deviceID).Scan(&keyID, &current, &name, &personID); err != nil {
		s.fail(w, http.StatusConflict, "This device is not set up yet.")
		return
	}
	var used int
	tx.QueryRowContext(ctx, `SELECT count(*) FROM device_keys WHERE public_key=?`, in.PublicKey).Scan(&used)
	if used > 0 {
		s.fail(w, http.StatusBadRequest, "That security code is already in use.")
		return
	}
	now := s.now()
	ts := now.Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `UPDATE device_keys SET state='retired', retired_at=? WHERE id=?`, ts, keyID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_keys(id,device_id,public_key,state,created_at,activated_at) VALUES(?,?,?,?,?,?)`,
		store.NewID(), deviceID, in.PublicKey, "active", ts, ts); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET refresh_due_at=?, refresh_failed_at=NULL WHERE id=?`, now.Add(agentRefreshAge).Format(time.RFC3339), deviceID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: "Wardline", Action: "device.refreshed", SubjectType: "device", SubjectID: deviceID,
		PlainText: audit.SentenceDeviceRefreshed(name), Details: map[string]any{"person_id": personID, "public_key": in.PublicKey}}); err != nil {
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
	s.hub.broadcast()
	s.json(w, http.StatusOK, map[string]any{"refreshed": true})
}

// handleAgentRollback puts back the key that was replaced, if that happened within the last hour.
func (s *Server) handleAgentRollback(w http.ResponseWriter, r *http.Request, deviceID string) {
	ctx := r.Context()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	now := s.now()
	ts := now.Format(time.RFC3339)
	var activeID, name, personID string
	if err := tx.QueryRowContext(ctx, `SELECT k.id, d.name, d.person_id FROM device_keys k JOIN devices d ON d.id=k.device_id
		WHERE k.device_id=? AND k.state='active'`, deviceID).Scan(&activeID, &name, &personID); err != nil {
		s.fail(w, http.StatusConflict, "This device is not set up yet.")
		return
	}
	var prevID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM device_keys WHERE device_id=? AND state='retired' AND retired_at >= ?
		ORDER BY retired_at DESC, rowid DESC LIMIT 1`, deviceID, now.Add(-refreshOverlap).Format(time.RFC3339)).Scan(&prevID)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, http.StatusConflict, "There is no earlier security code to go back to.")
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	steps := []struct {
		q    string
		args []any
	}{
		{`UPDATE device_keys SET state='retired', retired_at=? WHERE id=?`, []any{ts, activeID}},
		{`UPDATE device_keys SET state='active', retired_at=NULL WHERE id=?`, []any{prevID}},
		{`UPDATE devices SET refresh_failed_at=?, refresh_due_at=? WHERE id=?`, []any{ts, now.Add(refreshRetryAfter).Format(time.RFC3339), deviceID}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: "Wardline", Action: "device.refresh_failed", SubjectType: "device", SubjectID: deviceID,
		PlainText: audit.SentenceDeviceRefreshFailed(name), Details: map[string]any{"person_id": personID}}); err != nil {
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
	s.hub.broadcast()
	s.json(w, http.StatusOK, map[string]any{"rolled_back": true})
}

// handleAgentReport records what the device itself says about its connection. It can speak only for
// the device named by its own access token.
func (s *Server) handleAgentReport(w http.ResponseWriter, r *http.Request, deviceID string) {
	var in agent.Report
	if !s.decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	now := s.now()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	var name, personID, status string
	var seen sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT name, person_id, status, last_seen_at FROM devices WHERE id=? AND status <> 'removed'`, deviceID).
		Scan(&name, &personID, &status, &seen); err != nil {
		s.fail(w, http.StatusUnauthorized, "This device is not recognised. It may have been removed.")
		return
	}
	problem := in.Problem
	if len(problem) > 40 {
		problem = ""
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET agent_reported_at=?, agent_problem=? WHERE id=?`, now.Format(time.RFC3339), problem, deviceID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if !in.LastHandshake.IsZero() {
		hs := in.LastHandshake.UTC()
		if hs.After(now.Add(deviceFutureLeeway)) {
			hs = now
		}
		if prev := parseTime(seen); prev != nil && prev.After(hs) {
			hs = *prev
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at=?, status='active' WHERE id=?`, hs.Format(time.RFC3339), deviceID); err != nil {
			s.serverError(w, r, err)
			return
		}
		if status == "waiting" {
			if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: "Wardline", Action: "device.connected", SubjectType: "device", SubjectID: deviceID,
				PlainText: fmt.Sprintf("%s connected for the first time.", name), Details: map[string]any{"person_id": personID}}); err != nil {
				s.serverError(w, r, err)
				return
			}
		}
	}
	if v := in.AgentVersion; v != "" && len(v) <= 40 {
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET agent_version=? WHERE id=?`, v, deviceID); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
