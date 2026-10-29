package api

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"

	"lsl-wardline/internal/audit"
)

// A free DuckDNS name that follows this computer's address. Only offered when this computer is the
// server, because that is the only case where the address belongs to the machine Wardline runs on.

var duckNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

const duckSuffix = ".duckdns.org"

func (s *Server) dynamicDNSState(r *http.Request) map[string]any {
	out := map[string]any{"available": s.d.OnServerCreated != nil, "configured": false}
	var name string
	var okAt, ip, lastErr sql.NullString
	err := s.d.Store.DB().QueryRowContext(r.Context(), `SELECT name, last_ok_at, last_ip, last_error FROM dynamic_dns WHERE id=1`).Scan(&name, &okAt, &ip, &lastErr)
	if err != nil {
		return out
	}
	out["configured"] = true
	out["name"] = name
	out["full_name"] = name + duckSuffix
	out["last_ok_at"] = okAt.String
	out["last_ip"] = ip.String
	out["problem"] = lastErr.String
	return out
}

func (s *Server) handleDynamicDNSGet(w http.ResponseWriter, r *http.Request, _ string) {
	s.json(w, http.StatusOK, s.dynamicDNSState(r))
}

func (s *Server) handleDynamicDNSPut(w http.ResponseWriter, r *http.Request, adminID string) {
	if s.d.OnServerCreated == nil {
		s.fail(w, http.StatusConflict, "This is only available when this computer is your server.")
		return
	}
	var in struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	name := strings.ToLower(strings.TrimSpace(in.Name))
	token := strings.TrimSpace(in.Token)
	if !duckNameRe.MatchString(name) {
		s.fail(w, http.StatusBadRequest, "Enter just the name you chose at duckdns.org, like laney-network, without .duckdns.org or https://.")
		return
	}
	if len(token) < 8 || len(token) > 128 || strings.ContainsAny(token, " \t\r\n") {
		s.fail(w, http.StatusBadRequest, "Enter the token shown on your duckdns.org page. It is a long code with no spaces.")
		return
	}
	sealed, err := s.d.Box.SealPacked([]byte(token))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	ctx := r.Context()
	email := s.adminEmail(ctx, adminID)
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO dynamic_dns(id,provider,name,token_enc) VALUES(1,'duckdns',?,?)
		ON CONFLICT(id) DO UPDATE SET provider='duckdns', name=excluded.name, token_enc=excluded.token_enc, last_ok_at=NULL, last_ip=NULL, last_error=NULL`, name, sealed); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "network.name_kept", SubjectType: "network", SubjectID: "dynamic_dns",
		PlainText: audit.SentenceNameKept(email, name+duckSuffix)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	if s.d.DynamicDNSChanged != nil {
		s.d.DynamicDNSChanged()
	}
	s.json(w, http.StatusOK, s.dynamicDNSState(r))
}

func (s *Server) handleDynamicDNSDelete(w http.ResponseWriter, r *http.Request, adminID string) {
	if s.d.OnServerCreated == nil {
		s.fail(w, http.StatusConflict, "This is only available when this computer is your server.")
		return
	}
	ctx := r.Context()
	email := s.adminEmail(ctx, adminID)
	var name string
	if err := s.d.Store.DB().QueryRowContext(ctx, `SELECT name FROM dynamic_dns WHERE id=1`).Scan(&name); err != nil {
		s.json(w, http.StatusOK, s.dynamicDNSState(r)) // nothing to remove
		return
	}
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM dynamic_dns WHERE id=1`); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "network.name_removed", SubjectType: "network", SubjectID: "dynamic_dns",
		PlainText: audit.SentenceNameRemoved(email, name+duckSuffix)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, s.dynamicDNSState(r))
}
