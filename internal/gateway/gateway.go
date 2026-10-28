package gateway

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lsl-wardline/internal/wg"
)

// Control is how the server talks to the control plane.
type Control interface {
	Enroll(ctx context.Context, r EnrollRequest) (EnrollResponse, error)
	// Desired returns the signed state. With since >= 0 it waits (long poll)
	// until a version newer than since exists; changed is false on timeout.
	// With since < 0 it returns the current state at once.
	Desired(ctx context.Context, auth string, since int64) (s Signed, changed bool, err error)
	Report(ctx context.Context, auth string, r Report) error
}

// Identity is persisted in the state directory with owner-only permissions.
type Identity struct {
	ControlURL  string            `json:"control_url"`
	ServerID    string            `json:"server_id"`
	AuthToken   string            `json:"auth_token"`
	ControlKey  ed25519.PublicKey `json:"control_key"`
	PrivateKey  string            `json:"private_key"`
	PublicKey   string            `json:"public_key"`
	LastVersion int64             `json:"last_version"`
}

type Config struct {
	StateDir    string
	Iface       string
	Name        string
	EnrollToken string
	ControlURL  string
	Control     Control
	Backend     wg.Backend
	Log         *slog.Logger
	ReportEvery time.Duration
}

type Gateway struct {
	cfg     Config
	mu      sync.Mutex
	id      Identity
	started bool
	devices map[string]string // public key -> device id
}

func New(cfg Config) *Gateway {
	if cfg.ReportEvery == 0 {
		cfg.ReportEvery = 5 * time.Second
	}
	if cfg.Iface == "" {
		cfg.Iface = "wardline0"
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &Gateway{cfg: cfg, devices: map[string]string{}}
}

func (g *Gateway) idPath() string { return filepath.Join(g.cfg.StateDir, "identity.json") }

func (g *Gateway) saveIdentity() error {
	b, err := json.MarshalIndent(g.id, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(g.cfg.StateDir, 0o700); err != nil {
		return err
	}
	tmp := g.idPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, g.idPath())
}

// PublicKey is the server's public key.
func (g *Gateway) PublicKey() string { return g.id.PublicKey }

// Start loads the saved identity, or enrolls with the control plane the first time.
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started {
		return nil
	}
	if b, err := os.ReadFile(g.idPath()); err == nil {
		if err := json.Unmarshal(b, &g.id); err != nil {
			return fmt.Errorf("the saved server identity is damaged: %w", err)
		}
		g.started = true
		g.cfg.Log.Info("server identity loaded", "server_id", g.id.ServerID, "public_key", g.id.PublicKey)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if g.cfg.EnrollToken == "" {
		return errors.New("this server has not been connected yet; run the install command from the web app")
	}
	priv, pub, err := wg.GenerateKeypair()
	if err != nil {
		return err
	}
	res, err := g.cfg.Control.Enroll(ctx, EnrollRequest{Token: g.cfg.EnrollToken, PublicKey: pub, Name: g.cfg.Name})
	if err != nil {
		return fmt.Errorf("could not connect this server to the control plane: %w", err)
	}
	g.id = Identity{ControlURL: g.cfg.ControlURL, ServerID: res.ServerID, AuthToken: res.AuthToken, ControlKey: res.ControlKey,
		PrivateKey: priv, PublicKey: pub, LastVersion: -1}
	if err := g.saveIdentity(); err != nil {
		return err
	}
	g.started = true
	g.cfg.Log.Info("server enrolled", "server_id", g.id.ServerID, "public_key", pub)
	return nil
}

// Sync fetches the current desired state and applies it. It returns what changed.
func (g *Gateway) Sync(ctx context.Context) (wg.Diff, error) { return g.sync(ctx, -1) }

func (g *Gateway) sync(ctx context.Context, since int64) (wg.Diff, error) {
	if err := g.Start(ctx); err != nil {
		return wg.Diff{}, err
	}
	s, changed, err := g.cfg.Control.Desired(ctx, g.id.AuthToken, since)
	if err != nil || !changed {
		return wg.Diff{}, err
	}
	st, err := Verify(g.id.ControlKey, s)
	if err != nil {
		return wg.Diff{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if st.Version < g.id.LastVersion {
		return wg.Diff{}, ErrStaleState
	}
	return g.apply(ctx, st)
}

func (g *Gateway) apply(ctx context.Context, st DesiredState) (wg.Diff, error) {
	addr, err := netip.ParsePrefix(st.Address)
	if err != nil {
		return wg.Diff{}, fmt.Errorf("the control plane sent an invalid address: %w", err)
	}
	var desired []wg.PeerConfig
	devices := map[string]string{}
	for _, p := range st.Peers {
		ip, err := netip.ParseAddr(p.TunnelIP)
		if err != nil {
			return wg.Diff{}, fmt.Errorf("the control plane sent an invalid device address: %w", err)
		}
		desired = append(desired, wg.PeerConfig{
			PublicKey: p.PublicKey, PresharedKey: p.PresharedKey, Keepalive: p.Keepalive,
			AllowedIPs: []netip.Prefix{netip.PrefixFrom(ip, ip.BitLen())},
		})
		devices[p.PublicKey] = p.DeviceID
	}
	err = g.cfg.Backend.Configure(ctx, g.cfg.Iface, wg.InterfaceConfig{PrivateKey: g.id.PrivateKey, ListenPort: st.ListenPort, Address: addr})
	if err != nil {
		return wg.Diff{}, err
	}
	d, err := wg.Reconcile(ctx, g.cfg.Backend, g.cfg.Iface, desired)
	if err != nil {
		return d, err
	}
	g.devices = devices
	if st.Version != g.id.LastVersion {
		g.id.LastVersion = st.Version
		if err := g.saveIdentity(); err != nil {
			return d, err
		}
	}
	if !d.IsEmpty() {
		g.cfg.Log.Info("applied desired state", "version", st.Version, "added", len(d.Add), "changed", len(d.Update), "removed", len(d.Remove))
	}
	return d, nil
}

// ReportNow sends the current handshakes and traffic to the control plane.
func (g *Gateway) ReportNow(ctx context.Context) error {
	if err := g.Start(ctx); err != nil {
		return err
	}
	g.mu.Lock()
	version := g.id.LastVersion
	devices := make(map[string]string, len(g.devices))
	for k, v := range g.devices {
		devices[k] = v
	}
	g.mu.Unlock()

	r := Report{ServerID: g.id.ServerID, Time: time.Now().UTC(), Applied: version}
	// Before the first state is applied there is no interface yet; the report is then an empty heartbeat.
	peers, _ := g.cfg.Backend.Peers(ctx, g.cfg.Iface)
	for _, p := range peers {
		r.Peers = append(r.Peers, PeerReport{DeviceID: devices[p.PublicKey], PublicKey: p.PublicKey,
			LastHandshake: p.LastHandshake, RxBytes: p.RxBytes, TxBytes: p.TxBytes, Endpoint: p.LastEndpoint})
	}
	return g.cfg.Control.Report(ctx, g.id.AuthToken, r)
}

// Run keeps the server in step with the control plane until ctx ends.
func (g *Gateway) Run(ctx context.Context) error {
	if err := g.Start(ctx); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(g.cfg.ReportEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := g.ReportNow(ctx); err != nil && ctx.Err() == nil {
					g.cfg.Log.Warn("could not report to the control plane", "error", err.Error())
				}
			}
		}
	}()

	since := int64(-1) // first pass applies whatever exists, even if unchanged
	backoff := time.Second
	for ctx.Err() == nil {
		_, err := g.sync(ctx, since)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			g.cfg.Log.Warn("could not apply instructions from the control plane", "error", err.Error())
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		g.mu.Lock()
		since = g.id.LastVersion
		g.mu.Unlock()
		_ = g.ReportNow(ctx)
	}
	return ctx.Err()
}
