package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/render"
	"lsl-wardline/internal/store"
	"lsl-wardline/internal/wg"
)

const (
	connectedWindow   = 3 * time.Minute
	unseenAttention   = 30 * 24 * time.Hour
	manualRefreshAge  = 180 * 24 * time.Hour
	phoneKeepalive    = 0
	computerKeepalive = 25
)

var platformLabels = map[string]string{
	"ios": "iPhone", "android": "Android phone", "windows": "Windows computer", "macos": "Mac", "linux": "Linux computer",
}

var appLinks = map[string]string{
	"ios":     "https://apps.apple.com/us/app/wireguard/id1441195209",
	"android": "https://play.google.com/store/apps/details?id=com.wireguard.android",
	"windows": "https://www.wireguard.com/install/",
	"macos":   "https://apps.apple.com/us/app/wireguard/id1451685025",
	"linux":   "https://www.wireguard.com/install/",
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Server) personOf(ctx context.Context, q queryRower, deviceID string) string {
	var p string
	q.QueryRowContext(ctx, `SELECT person_id FROM devices WHERE id=?`, deviceID).Scan(&p)
	return p
}

// ---- people ----

func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request, _ string) {
	rows, err := s.d.Store.DB().QueryContext(r.Context(), `SELECT p.id, p.display_name,
		(SELECT count(*) FROM devices d WHERE d.person_id=p.id AND d.status <> 'removed') FROM people p ORDER BY p.display_name COLLATE NOCASE`)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var n int
		rows.Scan(&id, &name, &n)
		out = append(out, map[string]any{"id": id, "name": name, "device_count": n})
	}
	s.json(w, http.StatusOK, out)
}

func (s *Server) handlePersonCreate(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 60 {
		s.fail(w, http.StatusBadRequest, "Enter the person's name, up to 60 characters.")
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
	id := store.NewID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO people(id,network_id,display_name,created_at) VALUES(?,?,?,?)`, id, n.ID, name, s.now().Format(time.RFC3339)); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "person.added", SubjectType: "person", SubjectID: id,
		PlainText: fmt.Sprintf("%s added %s.", email, name), Details: map[string]any{"person_id": id}}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusCreated, map[string]any{"id": id, "name": name, "device_count": 0})
}

// ---- device views ----

type deviceRow struct {
	ID, PersonID, PersonName, Name, Platform, Kind, Status string
	TunnelIP, PublicKey, Endpoint                          string
	Added                                                  time.Time
	Seen, RefreshDue                                       *time.Time
	Rx, Tx                                                 int64
}

func (s *Server) loadDevices(ctx context.Context, where string, args ...any) ([]deviceRow, error) {
	rows, err := s.d.Store.DB().QueryContext(ctx, `SELECT d.id, d.person_id, p.display_name, d.name, d.platform, d.kind, d.status, d.tunnel_ip,
		COALESCE(k.public_key,''), COALESCE(d.last_endpoint,''), d.added_at, d.last_seen_at, d.refresh_due_at, d.rx_bytes, d.tx_bytes
		FROM devices d JOIN people p ON p.id=d.person_id LEFT JOIN device_keys k ON k.device_id=d.id AND k.state='active'
		WHERE d.status <> 'removed' `+where+` ORDER BY d.added_at, d.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deviceRow
	for rows.Next() {
		var d deviceRow
		var added string
		var seen, due sql.NullString
		if err := rows.Scan(&d.ID, &d.PersonID, &d.PersonName, &d.Name, &d.Platform, &d.Kind, &d.Status, &d.TunnelIP, &d.PublicKey, &d.Endpoint,
			&added, &seen, &due, &d.Rx, &d.Tx); err != nil {
			return nil, err
		}
		d.Added, _ = time.Parse(time.RFC3339, added)
		d.Seen, d.RefreshDue = parseTime(seen), parseTime(due)
		out = append(out, d)
	}
	return out, rows.Err()
}

// displayStatus is what people see: waiting, connected or not_seen.
func (s *Server) displayStatus(d deviceRow) string {
	switch {
	case d.Status == "waiting" || d.Seen == nil:
		return "waiting"
	case s.now().Sub(*d.Seen) <= connectedWindow:
		return "connected"
	default:
		return "not_seen"
	}
}

func (s *Server) view(d deviceRow) map[string]any {
	return map[string]any{
		"id": d.ID, "person_id": d.PersonID, "person_name": d.PersonName, "name": d.Name, "platform": d.Platform, "kind": d.Kind,
		"status": s.displayStatus(d), "last_seen_at": d.Seen, "added_at": d.Added,
		"refresh_due": d.RefreshDue != nil && !s.now().Before(*d.RefreshDue),
	}
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request, _ string) {
	ds, err := s.loadDevices(r.Context(), "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, d := range ds {
		out = append(out, s.view(d))
	}
	s.json(w, http.StatusOK, out)
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request, _ string) {
	ds, err := s.loadDevices(r.Context(), "AND d.id = ?", r.PathValue("id"))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(ds) == 0 {
		s.fail(w, http.StatusNotFound, "That device was not found. It may have been removed.")
		return
	}
	d := ds[0]
	v := s.view(d)
	v["advanced"] = map[string]any{"public_key": d.PublicKey, "address": d.TunnelIP, "received_bytes": d.Rx, "sent_bytes": d.Tx, "last_address_seen": d.Endpoint}
	s.json(w, http.StatusOK, v)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	n, err := s.network(ctx, s.d.Store.DB())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	ds, err := s.loadDevices(ctx, "")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	prow, err := s.d.Store.DB().QueryContext(ctx, `SELECT id, display_name FROM people ORDER BY display_name COLLATE NOCASE`)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	people := []map[string]any{}
	index := map[string]int{}
	for prow.Next() {
		var id, name string
		prow.Scan(&id, &name)
		index[id] = len(people)
		people = append(people, map[string]any{"id": id, "name": name, "devices": []map[string]any{}})
	}
	prow.Close()

	attention := []map[string]any{}
	now := s.now()
	for _, d := range ds {
		i := index[d.PersonID]
		people[i]["devices"] = append(people[i]["devices"].([]map[string]any), s.view(d))
		if d.Seen != nil && now.Sub(*d.Seen) > unseenAttention {
			attention = append(attention, map[string]any{"kind": "device_unseen_30_days", "device_id": d.ID, "device_name": d.Name})
		}
		if d.RefreshDue != nil && !now.Before(*d.RefreshDue) {
			attention = append(attention, map[string]any{"kind": "refresh_due", "device_id": d.ID, "device_name": d.Name})
		}
	}

	server := map[string]any{"status": "none"}
	var pub string
	var seen sql.NullString
	var sname string
	if err := s.d.Store.DB().QueryRowContext(ctx, `SELECT name, public_key, last_seen_at FROM servers WHERE role='active' ORDER BY rowid LIMIT 1`).Scan(&sname, &pub, &seen); err == nil {
		st := s.serverStatus(pub, parseTime(seen))
		server = map[string]any{"status": st, "name": sname}
		if st == "offline" {
			attention = append(attention, map[string]any{"kind": "server_offline"})
		}
	}
	s.json(w, http.StatusOK, map[string]any{
		"network":   map[string]any{"name": n.Name, "server_address": n.ServerAddress, "server_address_kind": n.Kind},
		"server":    server,
		"people":    people,
		"attention": attention,
	})
}

// ---- adding and removing devices ----

func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		PersonID string `json:"person_id"`
		Platform string `json:"platform"`
		Name     string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	label, ok := platformLabels[in.Platform]
	if !ok {
		s.fail(w, http.StatusBadRequest, "Choose what kind of device this is.")
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

	var personName string
	if err := tx.QueryRowContext(ctx, `SELECT display_name FROM people WHERE id=?`, in.PersonID).Scan(&personName); err != nil {
		s.fail(w, http.StatusBadRequest, "Choose whose device this is.")
		return
	}
	n, err := s.network(ctx, tx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if n.ServerAddress == "" {
		s.fail(w, http.StatusConflict, "Set the server address first, so the device knows where to connect.")
		return
	}
	var serverPub string
	var port int
	if err := tx.QueryRowContext(ctx, `SELECT public_key, listen_port FROM servers WHERE role='active' AND public_key <> '' ORDER BY rowid LIMIT 1`).Scan(&serverPub, &port); err != nil {
		s.fail(w, http.StatusConflict, "Your server has not checked in yet. Finish setting up your server, then add devices.")
		return
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = fmt.Sprintf("%s's %s", personName, label)
	}
	base, nth := name, 1
	for {
		var c int
		tx.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE network_id=? AND name=?`, n.ID, name).Scan(&c)
		if c == 0 {
			break
		}
		nth++
		name = fmt.Sprintf("%s %d", base, nth)
	}

	// The private key lives only in this function's memory and in the response.
	priv, pub, err := wg.GenerateKeypair()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	psk, err := wg.GeneratePresharedKey()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	sealed, nonce, err := s.d.Box.Seal([]byte(psk))
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	id := store.NewID()
	now := s.now()
	ts := now.Format(time.RFC3339)
	due := now.Add(manualRefreshAge).Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id,network_id,person_id,name,platform,kind,tunnel_ip,status,added_at,refresh_due_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, n.ID, in.PersonID, name, in.Platform, "manual", "pending-"+id, "waiting", ts, due); err != nil {
		s.serverError(w, r, err)
		return
	}
	ip, err := s.d.IPAM.Allocate(ctx, tx, n.ID, id)
	if err != nil {
		s.fail(w, http.StatusConflict, "Your network has no room for more devices.")
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET tunnel_ip=? WHERE id=?`, ip.String(), id); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_keys(id,device_id,public_key,state,created_at,activated_at) VALUES(?,?,?,?,?,?)`,
		store.NewID(), id, pub, "active", ts, ts); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO preshared_keys(device_id,ciphertext,nonce,created_at) VALUES(?,?,?,?)`, id, sealed, nonce, ts); err != nil {
		s.serverError(w, r, err)
		return
	}
	keepalive := computerKeepalive
	if in.Platform == "ios" || in.Platform == "android" {
		keepalive = phoneKeepalive
	}
	rng, _ := netip.ParsePrefix(n.Range)
	cfg, err := render.DeviceConfig(render.DeviceInput{PrivateKey: priv, PresharedKey: psk, TunnelIP: ip, NetworkRange: rng,
		ServerPublicKey: serverPub, ServerAddress: n.ServerAddress, ServerPort: port, Keepalive: keepalive})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	qr, err := render.QRCodePNG(cfg, 360)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "device.added", SubjectType: "device", SubjectID: id,
		PlainText: audit.SentenceDeviceAdded(email, name), Details: map[string]any{"person_id": in.PersonID, "platform": in.Platform, "public_key": pub}}); err != nil {
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

	ds, _ := s.loadDevices(ctx, "AND d.id = ?", id)
	var v map[string]any
	if len(ds) > 0 {
		v = s.view(ds[0])
	}
	// The one response that carries a private key. It is shown once and never stored.
	s.json(w, http.StatusCreated, map[string]any{
		"device": v, "config": cfg, "qr_png_base64": base64.StdEncoding.EncodeToString(qr),
		"filename": render.ConfigFileName(name), "app_link": appLinks[in.Platform], "one_time": true,
	})
}

func (s *Server) handleDeviceRemove(w http.ResponseWriter, r *http.Request, adminID string) {
	ctx := r.Context()
	id := r.PathValue("id")
	email := s.adminEmail(ctx, adminID)
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	var name, personID, networkID string
	err = tx.QueryRowContext(ctx, `SELECT name, person_id, network_id FROM devices WHERE id=? AND status <> 'removed'`, id).Scan(&name, &personID, &networkID)
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, http.StatusNotFound, "That device was not found. It may already have been removed.")
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	ts := s.now().Format(time.RFC3339)
	steps := []struct {
		q    string
		args []any
	}{
		{`UPDATE devices SET status='removed', removed_at=? WHERE id=?`, []any{ts, id}},
		{`UPDATE device_keys SET state='retired', retired_at=? WHERE device_id=? AND state <> 'retired'`, []any{ts, id}},
		{`DELETE FROM preshared_keys WHERE device_id=?`, []any{id}},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err := s.d.IPAM.Release(ctx, tx, networkID, id); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "device.removed", SubjectType: "device", SubjectID: id,
		PlainText: audit.SentenceDeviceRemoved(email, name), Details: map[string]any{"person_id": personID}}); err != nil {
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
	s.hub.broadcast() // servers hear about this at once, not on a schedule
	s.json(w, http.StatusOK, map[string]any{"removed": true})
}

// ---- activity ----

func (s *Server) activityFilter(r *http.Request) audit.Filter {
	q := r.URL.Query()
	f := audit.Filter{PersonID: q.Get("person_id"), SubjectID: q.Get("device_id")}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 1000 {
		f.Limit = n
	}
	return f
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request, _ string) {
	f := s.activityFilter(r)
	if f.Limit == 0 {
		f.Limit = 200
	}
	recs, err := s.d.Audit.List(r.Context(), f)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, e := range recs {
		out = append(out, map[string]any{"id": e.ID, "at": e.TS, "who": e.Actor, "text": e.PlainText})
	}
	s.json(w, http.StatusOK, out)
}

func (s *Server) handleActivityCSV(w http.ResponseWriter, r *http.Request, _ string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="wardline-activity.csv"`)
	if err := s.d.Audit.ExportCSV(r.Context(), w, s.activityFilter(r)); err != nil {
		s.d.Log.Error("activity export failed", "error", err.Error())
	}
}
