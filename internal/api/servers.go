package api

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/gateway"
	"lsl-wardline/internal/store"
	"lsl-wardline/internal/wg"
)

const (
	defaultListenPort = 51820
	onlineWindow      = 30 * time.Second
	enrollTokenTTL    = 24 * time.Hour
)

func parseTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return nil
	}
	return &t
}

func (s *Server) installInstructions(token string) map[string]string {
	url := s.d.PublicURL
	docker := fmt.Sprintf("docker run -d --name wardline-server --restart unless-stopped --cap-add NET_ADMIN --device /dev/net/tun -p %d:%d/udp -e WARDLINE_CONTROL_URL=%s -e WARDLINE_ENROLL_TOKEN=%s -v wardline-server:/var/lib/wardline lsl-wardline:latest gateway",
		defaultListenPort, defaultListenPort, url, token)
	compose := fmt.Sprintf(`services:
  wardline-server:
    image: lsl-wardline:latest
    command: ["gateway"]
    restart: unless-stopped
    cap_add: [NET_ADMIN]
    devices: ["/dev/net/tun:/dev/net/tun"]
    ports: ["%d:%d/udp"]
    environment:
      WARDLINE_CONTROL_URL: %s
      WARDLINE_ENROLL_TOKEN: %s
    volumes: ["wardline-server:/var/lib/wardline"]
volumes:
  wardline-server:
`, defaultListenPort, defaultListenPort, url, token)
	linux := fmt.Sprintf("sudo sh install-linux.sh gateway %s %s", url, token)
	return map[string]string{"linux": linux, "docker": docker, "compose": compose}
}

func (s *Server) handleServerCreate(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) // the name is optional, so an empty body is fine
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "My server"
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
	var existing int
	tx.QueryRowContext(ctx, `SELECT count(*) FROM servers WHERE role <> 'retired' AND public_key <> ''`).Scan(&existing)
	if existing > 0 {
		s.fail(w, http.StatusConflict, "You already have a server connected. Moving to a new server is coming in a later version.")
		return
	}
	// A server that never checked in can be replaced by a fresh install code.
	tx.ExecContext(ctx, `DELETE FROM enrollment_tokens WHERE server_id IN (SELECT id FROM servers WHERE public_key = '')`)
	tx.ExecContext(ctx, `DELETE FROM servers WHERE public_key = ''`)

	id, tok := store.NewID(), randToken()
	now := s.now()
	expires := now.Add(enrollTokenTTL)
	if _, err := tx.ExecContext(ctx, `INSERT INTO servers(id,network_id,name,public_key,listen_port,role,created_at) VALUES(?,?,?,?,?,?,?)`,
		id, n.ID, name, "", defaultListenPort, "active", now.Format(time.RFC3339)); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO enrollment_tokens(id,server_id,token_hash,expires_at) VALUES(?,?,?,?)`,
		store.NewID(), id, tokenHash(tok), expires.Format(time.RFC3339)); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "server.created", SubjectType: "server", SubjectID: id,
		PlainText: fmt.Sprintf("%s started setting up %s.", email, name)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusCreated, map[string]any{"id": id, "name": name, "expires_at": expires, "install": s.installInstructions(tok)})
}

func (s *Server) serverStatus(publicKey string, lastSeen *time.Time) string {
	switch {
	case publicKey == "":
		return "waiting"
	case lastSeen != nil && s.now().Sub(*lastSeen) <= onlineWindow:
		return "online"
	default:
		return "offline"
	}
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request, _ string) {
	rows, err := s.d.Store.DB().QueryContext(r.Context(), `SELECT id, name, public_key, listen_port, role, last_seen_at FROM servers WHERE role <> 'retired' ORDER BY rowid`)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, pub, role string
		var port int
		var seen sql.NullString
		if err := rows.Scan(&id, &name, &pub, &port, &role, &seen); err != nil {
			s.serverError(w, r, err)
			return
		}
		ls := parseTime(seen)
		out = append(out, map[string]any{"id": id, "name": name, "status": s.serverStatus(pub, ls), "last_seen_at": ls,
			"advanced": map[string]any{"public_key": pub, "listen_port": port}})
	}
	s.json(w, http.StatusOK, out)
}

// ---- endpoints used by servers ----

func (s *Server) handleGatewayEnroll(w http.ResponseWriter, r *http.Request) {
	var in gateway.EnrollRequest
	if !s.decode(w, r, &in) {
		return
	}
	bad := func() {
		s.fail(w, http.StatusUnauthorized, "That install code is not valid or has expired. Create a new one in Wardline.")
	}
	if wg.ValidateKey(in.PublicKey) != nil {
		s.fail(w, http.StatusBadRequest, "The server sent an invalid security code.")
		return
	}
	ctx := r.Context()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	var tokID, serverID, name, expires string
	var used sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT t.id, t.server_id, t.expires_at, t.used_at, s.name FROM enrollment_tokens t JOIN servers s ON s.id=t.server_id WHERE t.token_hash=?`,
		tokenHash(in.Token)).Scan(&tokID, &serverID, &expires, &used, &name)
	if errors.Is(err, sql.ErrNoRows) {
		bad()
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	exp, _ := time.Parse(time.RFC3339, expires)
	if used.Valid || !s.now().Before(exp) {
		bad()
		return
	}
	auth := randToken()
	now := s.now().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `UPDATE enrollment_tokens SET used_at=? WHERE id=?`, now, tokID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE servers SET public_key=?, auth_token_hash=?, last_seen_at=? WHERE id=?`, in.PublicKey, tokenHash(auth), now, serverID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: "Wardline", Action: "server.enrolled", SubjectType: "server", SubjectID: serverID,
		PlainText: audit.SentenceServerCheckedIn(name), Details: map[string]any{"public_key": in.PublicKey}}); err != nil {
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
	s.json(w, http.StatusOK, gateway.EnrollResponse{ServerID: serverID, AuthToken: auth, ControlKey: s.d.SigningKey.Public().(ed25519.PublicKey)})
}

// gateway authenticates a server by its bearer token.
func (s *Server) gateway(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		var id string
		err := s.d.Store.DB().QueryRowContext(r.Context(), `SELECT id FROM servers WHERE auth_token_hash=? AND role <> 'retired'`, tokenHash(tok)).Scan(&id)
		if tok == "" || err != nil {
			s.fail(w, http.StatusUnauthorized, "This server is not recognised. Set it up again from Wardline.")
			return
		}
		h(w, r, id)
	}
}

func (s *Server) version(ctx context.Context) int64 {
	var v string
	s.d.Store.DB().QueryRowContext(ctx, `SELECT value FROM settings WHERE key='desired_version'`).Scan(&v)
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func (s *Server) touchServer(ctx context.Context, id string) {
	s.d.Store.DB().ExecContext(ctx, `UPDATE servers SET last_seen_at=? WHERE id=?`, s.now().Format(time.RFC3339), id)
}

func (s *Server) handleGatewayDesired(w http.ResponseWriter, r *http.Request, serverID string) {
	s.touchServer(r.Context(), serverID)
	s.longPoll(w, r, func() { s.touchServer(r.Context(), serverID) }, func(v int64) (any, error) {
		st, err := s.buildState(r.Context(), serverID, v)
		if err != nil {
			return nil, err
		}
		return gateway.Sign(s.d.SigningKey, st)
	})
}

// buildState is what the server must look like right now.
func (s *Server) buildState(ctx context.Context, serverID string, version int64) (gateway.DesiredState, error) {
	n, err := s.network(ctx, s.d.Store.DB())
	if err != nil {
		return gateway.DesiredState{}, err
	}
	prefix, err := netip.ParsePrefix(n.Range)
	if err != nil {
		return gateway.DesiredState{}, err
	}
	sip, err := s.d.IPAM.ServerIP(ctx, s.d.Store.DB(), n.ID)
	if err != nil {
		return gateway.DesiredState{}, err
	}
	var port int
	if err := s.d.Store.DB().QueryRowContext(ctx, `SELECT listen_port FROM servers WHERE id=?`, serverID).Scan(&port); err != nil {
		return gateway.DesiredState{}, err
	}
	rows, err := s.d.Store.DB().QueryContext(ctx, `SELECT d.id, k.public_key, d.tunnel_ip, p.ciphertext, p.nonce
		FROM devices d JOIN device_keys k ON k.device_id=d.id AND k.state='active'
		LEFT JOIN preshared_keys p ON p.device_id=d.id
		WHERE d.status IN ('waiting','active') ORDER BY d.tunnel_ip`)
	if err != nil {
		return gateway.DesiredState{}, err
	}
	defer rows.Close()
	st := gateway.DesiredState{Version: version, IssuedAt: s.now(), ServerID: serverID,
		Address: netip.PrefixFrom(sip, prefix.Bits()).String(), ListenPort: port, Peers: []gateway.DesiredPeer{}}
	for rows.Next() {
		var p gateway.DesiredPeer
		var ct, nonce []byte
		if err := rows.Scan(&p.DeviceID, &p.PublicKey, &p.TunnelIP, &ct, &nonce); err != nil {
			return st, err
		}
		if len(ct) > 0 {
			pt, err := s.d.Box.Open(ct, nonce)
			if err != nil {
				return st, err
			}
			p.PresharedKey = string(pt)
		}
		st.Peers = append(st.Peers, p)
	}
	return st, rows.Err()
}

func (s *Server) handleGatewayReport(w http.ResponseWriter, r *http.Request, serverID string) {
	var in gateway.Report
	if !s.decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	s.touchServer(ctx, serverID)
	if len(in.Peers) > 10000 {
		s.fail(w, http.StatusBadRequest, "That report was too large.")
		return
	}
	now := s.now()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	for _, p := range in.Peers {
		if p.DeviceID == "" || p.LastHandshake.IsZero() {
			continue
		}
		var name, status, key string
		var seen sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT d.name, d.status, d.last_seen_at, k.public_key FROM devices d
			JOIN device_keys k ON k.device_id=d.id AND k.state='active' WHERE d.id=? AND d.status <> 'removed'`, p.DeviceID).Scan(&name, &status, &seen, &key)
		if err != nil || key != p.PublicKey {
			continue
		}
		hs := p.LastHandshake.UTC()
		if hs.After(now.Add(time.Minute)) {
			hs = now
		}
		if prev := parseTime(seen); prev != nil && prev.After(hs) {
			hs = *prev
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at=?, last_endpoint=?, rx_bytes=?, tx_bytes=?, status='active' WHERE id=?`,
			hs.Format(time.RFC3339), p.Endpoint, p.RxBytes, p.TxBytes, p.DeviceID); err != nil {
			s.serverError(w, r, err)
			return
		}
		if status == "waiting" {
			if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: "Wardline", Action: "device.connected", SubjectType: "device", SubjectID: p.DeviceID,
				PlainText: fmt.Sprintf("%s connected for the first time.", name), Details: map[string]any{"person_id": s.personOf(ctx, tx, p.DeviceID)}}); err != nil {
				s.serverError(w, r, err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
