package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/auth"
	"lsl-wardline/internal/ipam"
	"lsl-wardline/internal/render"
	"lsl-wardline/internal/store"
)

type network struct {
	ID, Name, Range, ServerAddress, Kind string
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Server) network(ctx context.Context, q queryer) (network, error) {
	var n network
	err := q.QueryRowContext(ctx, `SELECT id, name, address_range, server_address, server_address_kind FROM networks ORDER BY rowid LIMIT 1`).
		Scan(&n.ID, &n.Name, &n.Range, &n.ServerAddress, &n.Kind)
	return n, err
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"setup_done": false, "signed_in": false}
	n, err := s.network(r.Context(), s.d.Store.DB())
	if err == nil {
		out["setup_done"] = true
		out["network_name"] = n.Name
	}
	if id, ok := s.adminID(r); ok {
		out["signed_in"] = true
		out["email"] = s.adminEmail(r.Context(), id)
		out["two_step"], _ = s.accounts.HasTOTP(r.Context(), id)
	}
	s.json(w, http.StatusOK, out)
}

// handleSetup is the first-run screen: it creates the network and the one
// administrator in one step, and only while no administrator exists.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NetworkName string `json:"network_name"`
		Email       string `json:"email"`
		Password    string `json:"password"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	in.NetworkName = strings.TrimSpace(in.NetworkName)
	if in.NetworkName == "" || len(in.NetworkName) > 60 {
		s.fail(w, http.StatusBadRequest, "Give your network a name, up to 60 characters.")
		return
	}
	ctx := r.Context()
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback()

	adminID, err := s.accounts.Create(ctx, tx, in.Email, in.Password)
	if err != nil {
		if errors.Is(err, auth.ErrAdminExists) {
			s.fail(w, http.StatusConflict, "This network is already set up. Sign in instead.")
			return
		}
		s.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	prefix, err := ipam.ChooseRange(nil, ipam.DefaultRand())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	netID := store.NewID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO networks(id,name,address_range,server_address,server_address_kind,created_at) VALUES(?,?,?,?,?,?)`,
		netID, in.NetworkName, prefix.String(), "", "name", s.now().Format(time.RFC3339)); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.IPAM.Init(ctx, tx, netID, prefix); err != nil {
		s.serverError(w, r, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "network.created", SubjectType: "network", SubjectID: netID,
		PlainText: audit.SentenceNetworkCreated(email, in.NetworkName)}); err != nil {
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
	tok, err := s.sessions.Create(ctx, adminID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.setSessionCookie(w, tok, 0)
	s.json(w, http.StatusCreated, map[string]any{"network_name": in.NetworkName, "email": email})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	key := clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(in.Email))
	if ok, wait := s.limiter.Allow(key); !ok {
		mins := int(wait.Minutes()) + 1
		s.fail(w, http.StatusTooManyRequests, fmt.Sprintf("Too many sign-in attempts. Try again in %d minutes.", mins))
		return
	}
	ctx := r.Context()
	id, err := s.accounts.AuthenticateAt(ctx, in.Email, in.Password, strings.TrimSpace(in.Code), s.now())
	switch {
	case errors.Is(err, auth.ErrNeedCode):
		// Not a failure: the password was right and a code is needed next.
		s.json(w, http.StatusOK, map[string]any{"need_code": true})
		return
	case errors.Is(err, auth.ErrBadLogin):
		s.limiter.Fail(key)
		s.fail(w, http.StatusUnauthorized, err.Error())
		return
	case err != nil:
		s.serverError(w, r, err)
		return
	}
	s.limiter.Succeed(key)
	tok, err := s.sessions.Create(ctx, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	email := s.adminEmail(ctx, id)
	if err := s.d.Audit.Append(ctx, s.d.Store.DB(), audit.Event{Actor: email, Action: "admin.signed_in", SubjectType: "admin", SubjectID: id,
		PlainText: audit.SentenceAdminSignedIn(email)}); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.setSessionCookie(w, tok, 0)
	s.json(w, http.StatusOK, map[string]any{"email": email})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, _ string) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.Delete(r.Context(), c.Value)
	}
	s.setSessionCookie(w, "", -1)
	s.json(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleTOTPBegin(w http.ResponseWriter, r *http.Request, adminID string) {
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.pending.Store(adminID, secret)
	url := auth.TOTPURL(secret, s.adminEmail(r.Context(), adminID))
	qr, err := render.QRCodePNG(url, 280)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, map[string]string{"secret": secret, "url": url, "qr_png_base64": base64.StdEncoding.EncodeToString(qr)})
}

func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Code string `json:"code"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	v, ok := s.pending.Load(adminID)
	if !ok {
		s.fail(w, http.StatusConflict, "Start again to set up your authenticator app.")
		return
	}
	if err := s.accounts.EnableTOTP(r.Context(), adminID, v.(string), strings.TrimSpace(in.Code), s.now()); err != nil {
		s.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s.pending.Delete(adminID)
	email := s.adminEmail(r.Context(), adminID)
	s.d.Audit.Append(r.Context(), s.d.Store.DB(), audit.Event{Actor: email, Action: "admin.two_step_enabled", SubjectType: "admin", SubjectID: adminID,
		PlainText: email + " turned on the authenticator app for sign-in."})
	s.json(w, http.StatusOK, map[string]any{"two_step": true})
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request, _ string) {
	n, err := s.network(r.Context(), s.d.Store.DB())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, map[string]any{"id": n.ID, "name": n.Name, "server_address": n.ServerAddress,
		"server_address_kind": n.Kind, "suggested_address": s.suggestedAddress(), "advanced": map[string]string{"address_range": n.Range}})
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)

// classifyAddress returns "name" or "ip", or an error in plain language.
func classifyAddress(a string) (string, error) {
	a = strings.TrimSpace(a)
	if a == "" {
		return "", errors.New("Enter the name or address your devices will use to reach the server.")
	}
	if ip, err := netip.ParseAddr(a); err == nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return "", errors.New("That address only works on the server itself. Enter an address other devices can reach.")
		}
		return "ip", nil
	}
	if strings.ContainsAny(a, "/: \t") || !hostRe.MatchString(a) || len(a) > 253 {
		return "", errors.New("Enter just the name, like vpn.example.com, without http:// or a port number.")
	}
	return "name", nil
}

func (s *Server) handleServerAddress(w http.ResponseWriter, r *http.Request, adminID string) {
	var in struct {
		Address string `json:"address"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	in.Address = strings.TrimSpace(in.Address)
	kind, err := classifyAddress(in.Address)
	if err != nil {
		s.fail(w, http.StatusBadRequest, err.Error())
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
	if _, err := tx.ExecContext(ctx, `UPDATE networks SET server_address=?, server_address_kind=? WHERE id=?`, in.Address, kind, n.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.d.Audit.Append(ctx, tx, audit.Event{Actor: email, Action: "network.server_address_set", SubjectType: "network", SubjectID: n.ID,
		PlainText: fmt.Sprintf("%s set the server address to %s.", email, in.Address), Details: map[string]any{"kind": kind}}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.json(w, http.StatusOK, map[string]string{"server_address": in.Address, "server_address_kind": kind})
}

// suggestedAddress is the name or IP the web app itself is reached at, when that is something other
// devices could use too. It saves a non-technical person from looking up this computer's address.
func (s *Server) suggestedAddress() string {
	u, err := url.Parse(s.d.PublicURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" || host == "localhost" || addressProblem(host) {
		return ""
	}
	return host
}

func addressProblem(host string) bool { _, err := classifyAddress(host); return err != nil }
