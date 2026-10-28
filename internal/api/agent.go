package api

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"lsl-wardline/internal/agent"
	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/store"
	"lsl-wardline/internal/wg"
)

// ---- admin: add a computer that sets itself up ----

// createAgentDevice makes the device record and a one-time code. No key exists yet:
// the computer makes its own and sends only the public half when it enrolls.
func (s *Server) createAgentDevice(w http.ResponseWriter, r *http.Request, adminID, personID, platform, requestedName string) {
	if platform != "windows" && platform != "linux" {
		s.fail(w, http.StatusBadRequest, "One-click setup is available for Windows and Linux computers. For other devices, use the normal setup.")
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
	if err := tx.QueryRowContext(ctx, `SELECT display_name FROM people WHERE id=?`, personID).Scan(&personName); err != nil {
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
	var serverCount int
	tx.QueryRowContext(ctx, `SELECT count(*) FROM servers WHERE role='active' AND public_key <> ''`).Scan(&serverCount)
	if serverCount == 0 {
		s.fail(w, http.StatusConflict, "Your server has not checked in yet. Finish setting up your server, then add devices.")
		return
	}

	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = fmt.Sprintf("%s's %s", personName, platformLabels[platform])
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

	id, tok := store.NewID(), randToken()
	now := s.now()
	ts := now.Format(time.RFC3339)
	expires := now.Add(enrollTokenTTL)
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id,network_id,person_id,name,platform,kind,tunnel_ip,status,added_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, n.ID, personID, name, platform, "agent", "pending-"+id, "waiting", ts); err != nil {
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO preshared_keys(device_id,ciphertext,nonce,created_at) VALUES(?,?,?,?)`, id, sealed, nonce, ts); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO enrollment_tokens(id,device_id,token_hash,expires_at) VALUES(?,?,?,?)`,
		store.NewID(), id, tokenHash(tok), expires.Format(time.RFC3339)); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "device.added", SubjectType: "device", SubjectID: id,
		PlainText: audit.SentenceDeviceAdded(email, name), Details: map[string]any{"person_id": personID, "platform": platform}}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}

	ds, _ := s.loadDevices(ctx, "AND d.id = ?", id)
	var v map[string]any
	if len(ds) > 0 {
		v = s.view(ds[0])
	}
	s.json(w, http.StatusCreated, map[string]any{"device": v, "token": tok, "expires_at": expires, "install": s.agentInstall(platform, tok)})
}

func (s *Server) agentInstall(platform, token string) map[string]string {
	url := s.d.PublicURL
	if platform == "windows" {
		return map[string]string{"command": fmt.Sprintf("wardline.exe agent enroll --control-url %s --token %s", url, token)}
	}
	return map[string]string{"command": fmt.Sprintf("sudo sh install-linux.sh agent %s %s", url, token)}
}

// ---- endpoints used by agents ----

func (s *Server) handleAgentEnroll(w http.ResponseWriter, r *http.Request) {
	var in agent.EnrollRequest
	if !s.decode(w, r, &in) {
		return
	}
	if wg.ValidateKey(in.PublicKey) != nil {
		s.fail(w, http.StatusBadRequest, "The device sent an invalid security code.")
		return
	}
	bad := func() {
		s.fail(w, http.StatusUnauthorized, "That setup code is not valid or has expired. Add the device again in Wardline to get a new one.")
	}
	ctx := r.Context()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()
	var tokID, deviceID, name, personID, expires string
	var used sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT t.id, t.device_id, t.expires_at, t.used_at, d.name, d.person_id FROM enrollment_tokens t
		JOIN devices d ON d.id=t.device_id WHERE t.token_hash=? AND d.status <> 'removed' AND d.kind='agent'`, tokenHash(in.Token)).
		Scan(&tokID, &deviceID, &expires, &used, &name, &personID)
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
	authTok := randToken()
	ts := s.now().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `UPDATE enrollment_tokens SET used_at=? WHERE id=?`, ts, tokID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_keys(id,device_id,public_key,state,created_at,activated_at) VALUES(?,?,?,?,?,?)`,
		store.NewID(), deviceID, in.PublicKey, "active", ts, ts); err != nil {
		s.fail(w, http.StatusBadRequest, "That security code is already in use by another device.")
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET auth_token_hash=?, agent_version=? WHERE id=?`, tokenHash(authTok), in.AgentVersion, deviceID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: "Wardline", Action: "device.enrolled", SubjectType: "device", SubjectID: deviceID,
		PlainText: audit.SentenceDeviceSetUp(name), Details: map[string]any{"person_id": personID, "public_key": in.PublicKey}}); err != nil {
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
	s.hub.broadcast() // the server should start accepting this device straight away
	s.json(w, http.StatusOK, agent.EnrollResponse{DeviceID: deviceID, AuthToken: authTok, ControlKey: s.d.SigningKey.Public().(ed25519.PublicKey)})
}

// agentAuth authenticates a device by its bearer token. A removed device is no longer recognised.
func (s *Server) agentAuth(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearer(r)
		var id string
		err := s.d.Store.DB().QueryRowContext(r.Context(), `SELECT id FROM devices WHERE auth_token_hash=? AND status <> 'removed' AND kind='agent'`, tokenHash(tok)).Scan(&id)
		if tok == "" || err != nil {
			s.fail(w, http.StatusUnauthorized, "This device is not recognised. It may have been removed.")
			return
		}
		h(w, r, id)
	}
}

func (s *Server) handleAgentDesired(w http.ResponseWriter, r *http.Request, deviceID string) {
	s.longPoll(w, r, func() {}, func(v int64) (any, error) {
		st, err := s.buildAgentState(r.Context(), deviceID, v)
		if err != nil {
			return nil, err
		}
		return agent.Sign(s.d.SigningKey, st)
	})
}

// buildAgentState is how one computer must connect right now.
func (s *Server) buildAgentState(ctx context.Context, deviceID string, version int64) (agent.DesiredState, error) {
	n, err := s.network(ctx, s.d.Store.DB())
	if err != nil {
		return agent.DesiredState{}, err
	}
	prefix, err := netip.ParsePrefix(n.Range)
	if err != nil {
		return agent.DesiredState{}, err
	}
	var ip string
	var ct, nonce []byte
	if err := s.d.Store.DB().QueryRowContext(ctx, `SELECT d.tunnel_ip, p.ciphertext, p.nonce FROM devices d LEFT JOIN preshared_keys p ON p.device_id=d.id WHERE d.id=?`, deviceID).
		Scan(&ip, &ct, &nonce); err != nil {
		return agent.DesiredState{}, err
	}
	var serverPub string
	var port int
	if err := s.d.Store.DB().QueryRowContext(ctx, `SELECT public_key, listen_port FROM servers WHERE role='active' AND public_key <> '' ORDER BY rowid LIMIT 1`).Scan(&serverPub, &port); err != nil {
		return agent.DesiredState{}, err
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return agent.DesiredState{}, err
	}
	st := agent.DesiredState{
		Version: version, IssuedAt: s.now(), DeviceID: deviceID,
		Address:         netip.PrefixFrom(addr, prefix.Bits()).String(),
		ServerPublicKey: serverPub, Endpoint: net.JoinHostPort(n.ServerAddress, strconv.Itoa(port)),
		AllowedIPs: []string{prefix.String()}, Keepalive: computerKeepalive,
	}
	if len(ct) > 0 {
		pt, err := s.d.Box.Open(ct, nonce)
		if err != nil {
			return st, err
		}
		st.PresharedKey = string(pt)
	}
	return st, nil
}

// ---- shared by servers and agents ----

// longPoll answers "any news since version N?": it replies at once if there is some,
// otherwise it waits until something changes or the hold time passes (then 204).
// onWait runs when the wait times out, so the caller can note it was still alive.
func (s *Server) longPoll(w http.ResponseWriter, r *http.Request, onWait func(), build func(version int64) (any, error)) {
	since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if err != nil {
		since = -1
	}
	deadline := time.NewTimer(s.d.HoldTime)
	defer deadline.Stop()
	for {
		ch := s.hub.wait() // take the channel first so a change between here and the check is not missed
		v := s.version(r.Context())
		if since < 0 || v > since {
			out, err := build(v)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			s.json(w, http.StatusOK, out)
			return
		}
		select {
		case <-ch:
		case <-deadline.C:
			onWait()
			w.WriteHeader(http.StatusNoContent)
			return
		case <-r.Context().Done():
			return
		}
	}
}
