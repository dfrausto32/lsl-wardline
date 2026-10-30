// Package api is the control plane's HTTP interface: JSON for the web app and
// the /gateway/v1 endpoints that servers use.
package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"lsl-wardline/internal/audit"
	"lsl-wardline/internal/auth"
	"lsl-wardline/internal/ipam"
	"lsl-wardline/internal/portmap"
	"lsl-wardline/internal/store"
)

const sessionCookie = "wardline_session"

// Deps is everything the API needs.
type Deps struct {
	Store         *store.Store
	Audit         *audit.Log
	IPAM          *ipam.Allocator
	Box           *auth.SecretBox
	Log           *slog.Logger
	SigningKey    ed25519.PrivateKey
	PublicURL     string // how servers reach the control plane; shown in the install command
	Now           func() time.Time
	SessionTTL    time.Duration
	SecureCookies bool
	HoldTime      time.Duration // how long a server's "any news?" request waits (default 25s)
	Static        http.Handler  // the web app, served for everything that is not /api or /gateway
	// OnServerCreated, when set, means this computer is also the server: it is given the one-time install
	// code the moment it is made, so nobody has to copy a command. It must not block.
	OnServerCreated func(code string)
	// PublicIP finds the address the internet sees for this computer's network. It is only called when an
	// admin asks for it, and is replaced in tests.
	PublicIP func(ctx context.Context) (string, error)
	// OpenRouterPort asks the router to send the server port to this computer. Set only when this computer is the server.
	OpenRouterPort func(ctx context.Context) portmap.Result
}

type Server struct {
	d        Deps
	mux      *http.ServeMux
	accounts *auth.Accounts
	sessions *auth.Sessions
	limiter  *auth.Limiter
	hub      *hub
	pending  sync.Map // admin id -> pending TOTP secret (not stored until confirmed)
}

func New(d Deps) *Server {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.HoldTime == 0 {
		d.HoldTime = 25 * time.Second
	}
	if d.IPAM == nil {
		d.IPAM = &ipam.Allocator{DB: d.Store.DB(), Now: d.Now}
	}
	s := &Server{
		d:        d,
		mux:      http.NewServeMux(),
		accounts: auth.NewAccounts(d.Store.DB(), d.Box),
		sessions: &auth.Sessions{DB: d.Store.DB(), TTL: d.SessionTTL, Now: d.Now},
		limiter:  &auth.Limiter{Max: 5, Window: 15 * time.Minute, Now: d.Now},
		hub:      newHub(),
	}
	s.routes()
	// Machines made before names existed get theirs from their current names.
	if err := s.backfillShortNames(context.Background()); err != nil && d.Log != nil {
		d.Log.Error("could not give existing machines their names", "error", err.Error())
	}
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// EnsureSigningKey loads the key that signs instructions to servers, creating
// it on first run. It is kept encrypted in settings.
func EnsureSigningKey(ctx context.Context, st *store.Store, box *auth.SecretBox) (ed25519.PrivateKey, error) {
	var v string
	err := st.DB().QueryRowContext(ctx, `SELECT value FROM settings WHERE key='signing_key'`).Scan(&v)
	if err == nil {
		raw, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, err
		}
		seed, err := box.OpenPacked(raw)
		if err != nil {
			return nil, err
		}
		return ed25519.NewKeyFromSeed(seed), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	sealed, err := box.SealPacked(priv.Seed())
	if err != nil {
		return nil, err
	}
	_, err = st.DB().ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('signing_key',?)`, base64.StdEncoding.EncodeToString(sealed))
	return priv, err
}

func (s *Server) routes() {
	m := s.mux
	// Public
	m.HandleFunc("GET /api/state", s.handleState)
	m.HandleFunc("POST /api/setup", s.handleSetup)
	m.HandleFunc("POST /api/session", s.handleLogin)
	// Signed-in admin
	m.HandleFunc("DELETE /api/session", s.admin(s.handleLogout))
	m.HandleFunc("POST /api/totp/begin", s.admin(s.handleTOTPBegin))
	m.HandleFunc("POST /api/totp/confirm", s.admin(s.handleTOTPConfirm))
	m.HandleFunc("GET /api/network", s.admin(s.handleNetwork))
	m.HandleFunc("GET /api/network/public-address", s.admin(s.handlePublicAddress))
	m.HandleFunc("POST /api/network/router-forwarding", s.admin(s.handleRouterForwarding))
	m.HandleFunc("PUT /api/network/devices-reach-each-other", s.admin(s.handleReachPut))
	m.HandleFunc("PUT /api/network/server-address", s.admin(s.handleServerAddress))
	m.HandleFunc("GET /api/servers", s.admin(s.handleServers))
	m.HandleFunc("POST /api/servers", s.admin(s.handleServerCreate))
	m.HandleFunc("GET /api/people", s.admin(s.handlePeople))
	m.HandleFunc("POST /api/people", s.admin(s.handlePersonCreate))
	m.HandleFunc("GET /api/home", s.admin(s.handleHome))
	m.HandleFunc("GET /api/devices", s.admin(s.handleDevices))
	m.HandleFunc("POST /api/devices", s.admin(s.handleDeviceCreate))
	m.HandleFunc("GET /api/devices/{id}", s.admin(s.handleDevice))
	m.HandleFunc("DELETE /api/devices/{id}", s.admin(s.handleDeviceRemove))
	m.HandleFunc("PATCH /api/devices/{id}", s.admin(s.handleDeviceRename))
	m.HandleFunc("PUT /api/devices/{id}/names", s.admin(s.handleDeviceNames))
	m.HandleFunc("GET /api/names/preview", s.admin(s.handleNamePreview))
	m.HandleFunc("GET /api/activity", s.admin(s.handleActivity))
	m.HandleFunc("GET /api/activity.csv", s.admin(s.handleActivityCSV))
	// Servers
	m.HandleFunc("POST /gateway/v1/enroll", s.handleGatewayEnroll)
	m.HandleFunc("GET /gateway/v1/desired", s.gateway(s.handleGatewayDesired))
	m.HandleFunc("POST /gateway/v1/report", s.gateway(s.handleGatewayReport))
	// Computers that set themselves up
	m.HandleFunc("POST /agent/v1/enroll", s.handleAgentEnroll)
	m.HandleFunc("GET /agent/v1/desired", s.agentAuth(s.handleAgentDesired))
	m.HandleFunc("POST /agent/v1/report", s.agentAuth(s.handleAgentReport))
	m.HandleFunc("POST /agent/v1/refresh", s.agentAuth(s.handleAgentRefresh))
	m.HandleFunc("POST /agent/v1/refresh/rollback", s.agentAuth(s.handleAgentRollback))
	if s.d.Static != nil {
		m.Handle("/", s.d.Static)
	}
}

// ---- helpers ----

type ctxKey int

const (
	keyAdmin ctxKey = iota
	keyServer
)

func (s *Server) now() time.Time { return s.d.Now().UTC() }

func (s *Server) json(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

// fail sends a plain-language error. msg is shown to people, so it never carries internals.
func (s *Server) fail(w http.ResponseWriter, code int, msg string) {
	s.json(w, code, map[string]string{"error": msg})
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.d.Log.Error("request failed", "path", r.URL.Path, "error", err.Error())
	s.fail(w, http.StatusInternalServerError, "Something went wrong on our side. Try again in a moment.")
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		s.fail(w, http.StatusBadRequest, "That request was not understood.")
		return false
	}
	return true
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func (s *Server) adminID(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	id, err := s.sessions.Lookup(r.Context(), c.Value)
	return id, err == nil
}

// admin requires a signed-in administrator. State-changing requests must also
// carry X-Wardline, which a cross-site form cannot send.
func (s *Server) admin(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.adminID(r)
		if !ok {
			s.fail(w, http.StatusUnauthorized, "Please sign in to continue.")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Wardline") == "" {
			s.fail(w, http.StatusForbidden, "That request was blocked for your safety. Reload the page and try again.")
			return
		}
		h(w, r, id)
	}
}

func (s *Server) setSessionCookie(w http.ResponseWriter, tok string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: s.d.SecureCookies, MaxAge: maxAge})
}

func (s *Server) adminEmail(ctx context.Context, id string) string {
	var e string
	s.d.Store.DB().QueryRowContext(ctx, `SELECT email FROM admin_users WHERE id=?`, id).Scan(&e)
	return e
}

func randToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokenHash(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func bearer(r *http.Request) string {
	if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return v
	}
	return ""
}

// ---- change notification for servers' long polls ----

type hub struct {
	mu sync.Mutex
	ch chan struct{}
}

func newHub() *hub { return &hub{ch: make(chan struct{})} }

func (h *hub) wait() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ch
}

func (h *hub) broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	close(h.ch)
	h.ch = make(chan struct{})
}

// bumpVersion raises the desired-state version inside the caller's transaction.
func bumpVersion(ctx context.Context, q interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}) error {
	_, err := q.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('desired_version','1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1`)
	return err
}
