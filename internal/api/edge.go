package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/store"
)

// The read-only API for the Edge (specs/MEMBERSHIP-AND-MODES.md section 9, and the contract in the Edge
// project's specs/WARDLINE-CONTRACT.md). It reads and never writes, and a key made for it opens nothing else.

const (
	edgeScope       = "edge:read"
	edgeKeepEvents  = 1000
	maxLabels       = 20
	maxLabelValue   = 200
	edgeTokenPrefix = "wdl_edge_"
)

var labelKeyRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ---- keys for the Edge ----

func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 60 {
		s.fail(w, http.StatusBadRequest, "Give the key a name, up to 60 characters, so you can tell it apart later.")
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		s.serverError(w, r, err)
		return
	}
	token := edgeTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	ctx := r.Context()
	email := s.adminEmail(ctx, adminID)
	id := store.NewID()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO api_tokens(id,name,scope,token_hash,created_at) VALUES(?,?,?,?,?)`,
		id, name, edgeScope, tokenHash(token), s.now().Format(time.RFC3339)); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "token.created", SubjectType: "token", SubjectID: id,
		PlainText: audit.SentenceTokenCreated(email, name)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	// The only time the key is shown.
	s.json(w, http.StatusCreated, map[string]any{"id": id, "name": name, "scope": edgeScope, "token": token})
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request, _ string) {
	rows, err := s.d.Store.DB().QueryContext(r.Context(), `SELECT id, name, scope, created_at, last_used_at FROM api_tokens WHERE revoked_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, scope, created string
		var used sql.NullString
		if err := rows.Scan(&id, &name, &scope, &created, &used); err != nil {
			s.serverError(w, r, err)
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "scope": scope, "created_at": created, "last_used_at": parseTime(used)})
	}
	s.json(w, http.StatusOK, out)
}

func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request, adminID string) {
	ctx := r.Context()
	id := r.PathValue("id")
	email := s.adminEmail(ctx, adminID)
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM api_tokens WHERE id=? AND revoked_at IS NULL`, id).Scan(&name); err != nil {
		s.fail(w, http.StatusNotFound, "That key was not found. It may already have been removed.")
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at=? WHERE id=?`, s.now().Format(time.RFC3339), id); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "token.revoked", SubjectType: "token", SubjectID: id,
		PlainText: audit.SentenceTokenRevoked(email, name)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, map[string]any{"removed": true})
}

// edgeAuth lets a request through only with a good, unrevoked key that has the Edge's scope. A sign-in cookie does not count.
func (s *Server) edgeAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tok == "" {
			s.fail(w, http.StatusUnauthorized, "A key is needed to read this.")
			return
		}
		var id, scope string
		var revoked sql.NullString
		err := s.d.Store.DB().QueryRowContext(r.Context(), `SELECT id, scope, revoked_at FROM api_tokens WHERE token_hash=?`, tokenHash(tok)).Scan(&id, &scope, &revoked)
		if err != nil || revoked.Valid {
			s.fail(w, http.StatusUnauthorized, "That key is not valid.")
			return
		}
		if scope != edgeScope {
			s.fail(w, http.StatusForbidden, "That key is not allowed to read this.")
			return
		}
		now := s.now()
		s.d.Store.DB().ExecContext(r.Context(), `UPDATE api_tokens SET last_used_at=? WHERE id=? AND (last_used_at IS NULL OR last_used_at < ?)`,
			now.Format(time.RFC3339), id, now.Add(-time.Minute).Format(time.RFC3339))
		next(w, r)
	}
}

// ---- labels ----

// handleDeviceLabels replaces all of a machine's labels. Wardline keeps and returns them and never reads them.
func (s *Server) handleDeviceLabels(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Labels map[string]string `json:"labels"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if len(in.Labels) > maxLabels {
		s.fail(w, http.StatusBadRequest, fmt.Sprintf("A machine can have up to %d labels.", maxLabels))
		return
	}
	for k, v := range in.Labels {
		if !labelKeyRE.MatchString(k) {
			s.fail(w, http.StatusBadRequest, "A label name uses lowercase letters, numbers, dots, hyphens and underscores, up to 63 characters.")
			return
		}
		if len([]rune(v)) > maxLabelValue {
			s.fail(w, http.StatusBadRequest, fmt.Sprintf("A label's value can be up to %d characters.", maxLabelValue))
			return
		}
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
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM devices WHERE id=? AND status <> 'removed'`, id).Scan(&name); err != nil {
		s.fail(w, http.StatusNotFound, "That machine was not found. It may have been removed.")
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM member_labels WHERE member_id=?`, id); err != nil {
		s.serverError(w, r, err)
		return
	}
	for k, v := range in.Labels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO member_labels(member_id,key,value) VALUES(?,?,?)`, id, k, v); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "device.labels_changed", SubjectType: "device", SubjectID: id,
		PlainText: audit.SentenceDeviceLabels(email, name)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, map[string]any{"labels": in.Labels})
}

// ---- the member list ----

type edgeMember struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	HostnameLabel  string            `json:"hostname_label"`
	OverlayAddress string            `json:"overlay_address"`
	Roles          []string          `json:"roles"`
	Status         string            `json:"status"`
	LastSeenAt     *string           `json:"last_seen_at,omitempty"`
	Labels         map[string]string `json:"labels"`
}

// fingerprint is everything about a member except its status and when it was last seen: a change in it is a "member_updated".
func (m edgeMember) fingerprint() string {
	b, _ := json.Marshal(map[string]any{"name": m.Name, "label": m.HostnameLabel, "address": m.OverlayAddress, "roles": m.Roles, "labels": m.Labels})
	return string(b)
}

func (s *Server) edgeStatus(seen *time.Time, waiting bool) string {
	switch {
	case waiting || seen == nil:
		return "waiting"
	case s.now().Sub(*seen) <= connectedWindow:
		return "connected"
	default:
		return "not_seen_recently"
	}
}

func (s *Server) edgeMembers(ctx context.Context) (groupID string, out []edgeMember, err error) {
	db := s.d.Store.DB()
	if err := db.QueryRowContext(ctx, `SELECT id FROM groups ORDER BY rowid LIMIT 1`).Scan(&groupID); err != nil {
		return "", nil, errors.New("the network has no group yet")
	}
	labels := map[string]map[string]string{}
	lrows, err := db.QueryContext(ctx, `SELECT member_id, key, value FROM member_labels`)
	if err != nil {
		return "", nil, err
	}
	for lrows.Next() {
		var id, k, v string
		if err := lrows.Scan(&id, &k, &v); err != nil {
			lrows.Close()
			return "", nil, err
		}
		if labels[id] == nil {
			labels[id] = map[string]string{}
		}
		labels[id][k] = v
	}
	lrows.Close()
	labelsFor := func(id string) map[string]string {
		if l := labels[id]; l != nil {
			return l
		}
		return map[string]string{}
	}

	taken := map[string]bool{}
	drows, err := db.QueryContext(ctx, `SELECT id, name, COALESCE(short_name,''), tunnel_ip, status, last_seen_at FROM devices WHERE status <> 'removed' ORDER BY added_at, id`)
	if err != nil {
		return "", nil, err
	}
	for drows.Next() {
		var m edgeMember
		var status string
		var seen sql.NullString
		if err := drows.Scan(&m.ID, &m.Name, &m.HostnameLabel, &m.OverlayAddress, &status, &seen); err != nil {
			drows.Close()
			return "", nil, err
		}
		t := parseTime(seen)
		m.Status = s.edgeStatus(t, status == "waiting")
		if t != nil {
			ts := t.UTC().Format(time.RFC3339)
			m.LastSeenAt = &ts
		}
		m.Roles, m.Labels = []string{}, labelsFor(m.ID)
		taken[m.HostnameLabel] = true
		out = append(out, m)
	}
	drows.Close()

	n, err := s.network(ctx, db)
	if err != nil {
		return "", nil, err
	}
	sip, err := s.d.IPAM.ServerIP(ctx, db, n.ID)
	if err != nil {
		return "", nil, err
	}
	srows, err := db.QueryContext(ctx, `SELECT id, name, last_seen_at FROM servers WHERE role='active' AND public_key <> '' ORDER BY created_at, id`)
	if err != nil {
		return "", nil, err
	}
	defer srows.Close()
	label, i := "server", 1
	for srows.Next() {
		var m edgeMember
		var seen sql.NullString
		if err := srows.Scan(&m.ID, &m.Name, &seen); err != nil {
			return "", nil, err
		}
		for taken[label] {
			i++
			label = "server-" + strconv.Itoa(i)
		}
		taken[label] = true
		m.HostnameLabel, m.OverlayAddress = label, sip.String()
		t := parseTime(seen)
		m.Status = s.edgeStatus(t, false)
		if t != nil {
			ts := t.UTC().Format(time.RFC3339)
			m.LastSeenAt = &ts
		}
		m.Roles, m.Labels = []string{"gateway"}, labelsFor(m.ID)
		out = append(out, m)
	}
	return groupID, out, srows.Err()
}

// edgeSync finds what changed since it last looked and records it as events. Nothing happens on a first look,
// which just notes the starting point. It returns the number of the newest event.
func (s *Server) edgeSync(ctx context.Context) (int64, error) {
	s.edgeMu.Lock()
	defer s.edgeMu.Unlock()
	_, now, err := s.edgeMembers(ctx)
	if err != nil {
		return 0, err
	}
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var snap sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT data FROM edge_snapshot WHERE id=1`).Scan(&snap)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	known := err == nil && snap.Valid
	if known {
		var prevList []edgeMember
		if err := json.Unmarshal([]byte(snap.String), &prevList); err != nil {
			return 0, err
		}
		prev := map[string]edgeMember{}
		for _, m := range prevList {
			prev[m.ID] = m
		}
		cur := map[string]edgeMember{}
		for _, m := range now {
			cur[m.ID] = m
		}
		ts := s.now().UTC().Format(time.RFC3339)
		emit := func(kind string, m edgeMember) error {
			b, err := json.Marshal(m)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO edge_events(kind,member_id,data,created_at) VALUES(?,?,?,?)`, kind, m.ID, string(b), ts)
			return err
		}
		ids := make([]string, 0, len(now))
		for _, m := range now {
			ids = append(ids, m.ID)
		}
		sort.Strings(ids)
		for _, id := range ids {
			m, p := cur[id], prev[id]
			if _, existed := prev[id]; !existed {
				if err := emit("member_added", m); err != nil {
					return 0, err
				}
				continue
			}
			if p.fingerprint() != m.fingerprint() {
				if err := emit("member_updated", m); err != nil {
					return 0, err
				}
			}
			if p.Status != m.Status {
				if err := emit("member_status_changed", m); err != nil {
					return 0, err
				}
			}
		}
		gone := []string{}
		for id := range prev {
			if _, still := cur[id]; !still {
				gone = append(gone, id)
			}
		}
		sort.Strings(gone)
		for _, id := range gone {
			m := prev[id]
			m.Status = "removed"
			if err := emit("member_removed", m); err != nil {
				return 0, err
			}
		}
	}
	b, err := json.Marshal(now)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO edge_snapshot(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, string(b)); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM edge_events WHERE seq <= (SELECT COALESCE(MAX(seq),0) FROM edge_events) - ?`, edgeKeepEvents); err != nil {
		return 0, err
	}
	var newest int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM edge_events`).Scan(&newest); err != nil {
		return 0, err
	}
	return newest, tx.Commit()
}

func (s *Server) handleEdgeMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cursor, err := s.edgeSync(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	gid, members, err := s.edgeMembers(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if members == nil {
		members = []edgeMember{}
	}
	s.json(w, http.StatusOK, map[string]any{"version": "v1", "group_id": gid, "generated_at": s.now().UTC().Format(time.RFC3339),
		"cursor": strconv.FormatInt(cursor, 10), "members": members})
}

// ---- the event stream ----

func (s *Server) handleEdgeEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, http.StatusInternalServerError, "This server cannot stream events.")
		return
	}
	newest, err := s.edgeSync(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var oldest int64
	if err := s.d.Store.DB().QueryRowContext(ctx, `SELECT COALESCE(MIN(seq), ?) FROM edge_events`, newest+1).Scan(&oldest); err != nil {
		s.serverError(w, r, err)
		return
	}
	last := newest
	text := r.Header.Get("Last-Event-ID")
	if text == "" {
		text = r.URL.Query().Get("last_event_id")
	}
	if text != "" {
		n, err := strconv.ParseInt(text, 10, 64)
		// An id from the future, from before the oldest event kept, or that is not ours means events may have
		// been missed: say so, and the Edge fetches the whole list again.
		if err != nil || n > newest || n < oldest-1 {
			s.fail(w, http.StatusGone, "That point in the changes is too old. Fetch the whole member list again.")
			return
		}
		last = n
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	poll := time.NewTicker(s.edgePoll)
	defer poll.Stop()
	beat := time.NewTicker(s.edgeHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-beat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-poll.C:
			if _, err := s.edgeSync(ctx); err != nil {
				return
			}
			rows, err := s.d.Store.DB().QueryContext(ctx, `SELECT seq, kind, data FROM edge_events WHERE seq > ? ORDER BY seq`, last)
			if err != nil {
				return
			}
			wrote := false
			for rows.Next() {
				var seq int64
				var kind, data string
				if err := rows.Scan(&seq, &kind, &data); err != nil {
					break
				}
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, kind, data)
				last, wrote = seq, true
			}
			rows.Close()
			if wrote {
				flusher.Flush()
			}
		}
	}
}
