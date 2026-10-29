package api

import (
	"net/http"

	"lsl-wardline/internal/audit"
)

// handleReachPut turns on or off letting devices reach each other through the server. Off is the
// default; the servers hear about the change at once and relay (or stop relaying) between devices.
func (s *Server) handleReachPut(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	n, err := s.network(ctx, s.d.Store.DB())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	email := s.adminEmail(ctx, adminID)
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	v := 0
	if in.Enabled {
		v = 1
	}
	if _, err := tx.ExecContext(ctx, `UPDATE networks SET devices_reach_each_other=? WHERE id=?`, v, n.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := bumpVersion(ctx, tx); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "network.reach_changed", SubjectType: "network", SubjectID: n.ID,
		PlainText: audit.SentenceReachChanged(email, in.Enabled)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.hub.broadcast()
	s.json(w, http.StatusOK, map[string]any{"devices_reach_each_other": in.Enabled})
}
