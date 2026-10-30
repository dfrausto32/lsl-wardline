package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lsl-wardline/internal/gateway"
	"lsl-wardline/internal/wg"
)

var (
	// ErrRevoked means the control plane no longer recognises this device: it was removed.
	ErrRevoked = errors.New("this device has been removed from the network")
	// ErrRefreshFailed means a security refresh did not work and the old key is back in use.
	ErrRefreshFailed = errors.New("the security refresh did not work; the old one is still in use")
	// ErrKeyMismatch means the control plane expects a key this device does not hold.
	ErrKeyMismatch = errors.New("the control plane expects a different security key than this device has")
)

const (
	connectedWindow  = 3 * time.Minute
	unreachableAfter = 5 * time.Minute // with a handshake in the past
	firstConnectWait = 2 * time.Minute // with no handshake yet
)

// Control is how the agent talks to the control plane.
type Control interface {
	Enroll(ctx context.Context, r EnrollRequest) (EnrollResponse, error)
	// Desired returns the signed instructions. With since >= 0 it waits until something newer exists;
	// changed is false on timeout. A removed device gets ErrRevoked.
	Desired(ctx context.Context, auth string, since int64) (s gateway.Signed, changed bool, err error)
	Report(ctx context.Context, auth string, r Report) error
	// Refresh asks the control plane to accept newPublicKey in place of the current key.
	Refresh(ctx context.Context, auth string, newPublicKey string) error
	// Rollback asks it to go back to the previous key.
	Rollback(ctx context.Context, auth string) error
}

// Identity is kept in the state folder with owner-only permissions. The private keys
// in it never leave this computer.
type Identity struct {
	ControlURL  string            `json:"control_url"`
	DeviceID    string            `json:"device_id"`
	AuthToken   string            `json:"auth_token"`
	ControlKey  ed25519.PublicKey `json:"control_key"`
	PrivateKey  string            `json:"private_key"`
	PublicKey   string            `json:"public_key"`
	LastVersion int64             `json:"last_version"`
	// A key being introduced by a refresh, saved before the control plane is asked so a crash loses nothing.
	PendingPrivateKey string `json:"pending_private_key,omitempty"`
	PendingPublicKey  string `json:"pending_public_key,omitempty"`
	// The key being replaced, kept only until the new one is shown to work.
	PreviousPrivateKey string `json:"previous_private_key,omitempty"`
	PreviousPublicKey  string `json:"previous_public_key,omitempty"`
}

type Config struct {
	StateDir    string
	Iface       string
	ControlURL  string
	EnrollToken string // only needed on the first start
	Control     Control
	Backend     wg.Backend
	Log         *slog.Logger
	Version     string
	// Resolver turns a server name into addresses. Defaults to the system resolver.
	Resolver func(ctx context.Context, host string) ([]netip.Addr, error)
	Now      func() time.Time

	ReportEvery       time.Duration // default 30s
	ResolveEvery      time.Duration // default 60s
	RefreshCheckEvery time.Duration // default 5m
	HandshakeWait     time.Duration // how long a new key gets to prove itself; default 30s

	// StatusPath, when set, is a file the agent keeps up to date for the tray icon and desktop window.
	StatusPath string
}

type Agent struct {
	cfg  Config
	opMu sync.Mutex // one change to the tunnel at a time
	mu   sync.Mutex // guards the fields below
	id   Identity
	// started is true once an identity is loaded or created.
	started   bool
	last      *DesiredState
	endpoint  string // the resolved address currently applied
	appliedAt time.Time
	prob      string
	attempted time.Time // last refresh attempt
}

func New(cfg Config) *Agent {
	if cfg.Iface == "" {
		cfg.Iface = "wardline-agent"
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Resolver == nil {
		cfg.Resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	if cfg.ReportEvery == 0 {
		cfg.ReportEvery = 30 * time.Second
	}
	if cfg.ResolveEvery == 0 {
		cfg.ResolveEvery = time.Minute
	}
	if cfg.RefreshCheckEvery == 0 {
		cfg.RefreshCheckEvery = 5 * time.Minute
	}
	if cfg.HandshakeWait == 0 {
		cfg.HandshakeWait = 30 * time.Second
	}
	return &Agent{cfg: cfg}
}

// ---- identity ----

func (a *Agent) idPath() string { return filepath.Join(a.cfg.StateDir, "agent.json") }

func (a *Agent) identity() Identity {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id
}

func (a *Agent) problem() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prob
}

func (a *Agent) setProblem(p string) {
	a.mu.Lock()
	a.prob = p
	a.mu.Unlock()
}

// saveLocked writes the identity. The caller holds a.mu.
func (a *Agent) saveLocked() error {
	b, err := json.MarshalIndent(a.id, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.cfg.StateDir, 0o700); err != nil {
		return err
	}
	tmp := a.idPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.idPath())
}

// Start loads the saved identity, or enrolls with the control plane the first time.
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return nil
	}
	if b, err := os.ReadFile(a.idPath()); err == nil {
		if err := json.Unmarshal(b, &a.id); err != nil {
			return fmt.Errorf("the saved device identity is damaged: %w", err)
		}
		a.started = true
		a.cfg.Log.Info("device identity loaded", "device_id", a.id.DeviceID)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if a.cfg.EnrollToken == "" {
		return errors.New("this computer has not been set up yet; run the install command from the web app")
	}
	priv, pub, err := wg.GenerateKeypair()
	if err != nil {
		return err
	}
	res, err := a.cfg.Control.Enroll(ctx, EnrollRequest{Token: a.cfg.EnrollToken, PublicKey: pub, AgentVersion: a.cfg.Version})
	if err != nil {
		return fmt.Errorf("could not set up this computer: %w", err)
	}
	a.id = Identity{ControlURL: a.cfg.ControlURL, DeviceID: res.DeviceID, AuthToken: res.AuthToken, ControlKey: res.ControlKey,
		PrivateKey: priv, PublicKey: pub, LastVersion: -1}
	if err := a.saveLocked(); err != nil {
		return err
	}
	a.started = true
	a.cfg.Log.Info("device enrolled", "device_id", a.id.DeviceID)
	return nil
}

// ---- following the control plane ----

// Sync fetches the current instructions and applies them. It returns what changed in the tunnel.
func (a *Agent) Sync(ctx context.Context) (wg.Diff, error) { return a.sync(ctx, -1) }

func (a *Agent) sync(ctx context.Context, since int64) (wg.Diff, error) {
	if err := a.Start(ctx); err != nil {
		return wg.Diff{}, err
	}
	id := a.identity()
	s, changed, err := a.cfg.Control.Desired(ctx, id.AuthToken, since)
	if err != nil || !changed {
		return wg.Diff{}, err
	}
	st, err := Verify(id.ControlKey, s)
	if err != nil {
		return wg.Diff{}, err
	}
	return a.applyState(ctx, st)
}

func (a *Agent) applyState(ctx context.Context, st DesiredState) (wg.Diff, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	if st.Version < a.id.LastVersion {
		a.mu.Unlock()
		return wg.Diff{}, gateway.ErrStaleState
	}
	err := a.followKeyLocked(st)
	a.mu.Unlock()
	if err != nil {
		return wg.Diff{}, err
	}
	d, err := a.apply(ctx, st)
	if err != nil {
		return d, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if st.Version != a.id.LastVersion {
		a.id.LastVersion = st.Version
		if err := a.saveLocked(); err != nil {
			return d, err
		}
	}
	if !d.IsEmpty() {
		a.cfg.Log.Info("applied instructions", "version", st.Version, "added", len(d.Add), "changed", len(d.Update), "removed", len(d.Remove))
	}
	return d, nil
}

// followKeyLocked lines the agent's key up with the one the control plane accepts. The control plane decides.
func (a *Agent) followKeyLocked(st DesiredState) error {
	id := &a.id
	switch {
	case st.PublicKey == "" || st.PublicKey == id.PublicKey:
		if id.PendingPublicKey != "" { // a refresh was started but never accepted
			id.PendingPrivateKey, id.PendingPublicKey = "", ""
			return a.saveLocked()
		}
	case id.PendingPublicKey != "" && st.PublicKey == id.PendingPublicKey: // accepted, but we died before switching
		id.PrivateKey, id.PublicKey = id.PendingPrivateKey, id.PendingPublicKey
		id.PendingPrivateKey, id.PendingPublicKey = "", ""
		id.PreviousPrivateKey, id.PreviousPublicKey = "", ""
		return a.saveLocked()
	case id.PreviousPublicKey != "" && st.PublicKey == id.PreviousPublicKey: // rolled back while we were away
		id.PrivateKey, id.PublicKey = id.PreviousPrivateKey, id.PreviousPublicKey
		id.PreviousPrivateKey, id.PreviousPublicKey = "", ""
		return a.saveLocked()
	default:
		a.prob = ProblemKeyMismatch
		return ErrKeyMismatch
	}
	if a.prob == ProblemKeyMismatch {
		a.prob = ProblemNone
	}
	return nil
}

// endpointFor turns host:port into ip:port. An address needs no lookup.
func (a *Agent) endpointFor(ctx context.Context, hostport string) (string, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", fmt.Errorf("the control plane sent an invalid server address: %w", err)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return net.JoinHostPort(ip.String(), port), nil
	}
	ips, err := a.cfg.Resolver(ctx, host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("could not find the server at %s", host)
	}
	pick := ips[0]
	for _, ip := range ips {
		if ip.Is4() {
			pick = ip
			break
		}
	}
	return net.JoinHostPort(pick.Unmap().String(), port), nil
}

// apply makes the tunnel match st. The caller holds opMu.
func (a *Agent) apply(ctx context.Context, st DesiredState) (wg.Diff, error) {
	addr, err := netip.ParsePrefix(st.Address)
	if err != nil {
		return wg.Diff{}, fmt.Errorf("the control plane sent an invalid address: %w", err)
	}
	var allowed []netip.Prefix
	for _, s := range st.AllowedIPs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return wg.Diff{}, fmt.Errorf("the control plane sent an invalid address range: %w", err)
		}
		allowed = append(allowed, p)
	}
	ep, err := a.endpointFor(ctx, st.Endpoint)
	if err != nil {
		a.mu.Lock()
		if a.last != nil { // the tunnel keeps working on the last address that worked
			a.prob = ProblemNameNotFound
		}
		a.mu.Unlock()
		return wg.Diff{}, err
	}
	a.mu.Lock()
	priv := a.id.PrivateKey
	a.mu.Unlock()
	if err := a.cfg.Backend.Configure(ctx, a.cfg.Iface, wg.InterfaceConfig{PrivateKey: priv, Address: addr}); err != nil {
		return wg.Diff{}, err
	}
	d, err := wg.Reconcile(ctx, a.cfg.Backend, a.cfg.Iface, []wg.PeerConfig{{
		PublicKey: st.ServerPublicKey, PresharedKey: st.PresharedKey, AllowedIPs: allowed, Keepalive: st.Keepalive, Endpoint: ep,
	}})
	if err != nil {
		return d, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.last == nil || a.endpoint != ep || a.last.ServerPublicKey != st.ServerPublicKey {
		a.appliedAt = a.cfg.Now()
	}
	cp := st
	a.last, a.endpoint = &cp, ep
	if a.prob == ProblemNameNotFound {
		a.prob = ProblemNone
	}
	return d, nil
}

// ResolveNow looks the server's name up again and follows it if it moved. If the name cannot be
// found the tunnel keeps using the last address that worked.
func (a *Agent) ResolveNow(ctx context.Context) (wg.Diff, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	last := a.last
	a.mu.Unlock()
	if last == nil {
		return wg.Diff{}, nil
	}
	return a.apply(ctx, *last)
}

// ---- security refresh ----

// RefreshIfDue starts a refresh when the control plane says it is time, at most once an hour.
func (a *Agent) RefreshIfDue(ctx context.Context) error {
	a.mu.Lock()
	last, attempted := a.last, a.attempted
	a.mu.Unlock()
	if last == nil || last.RefreshDue == nil || a.cfg.Now().Before(*last.RefreshDue) {
		return nil
	}
	if !attempted.IsZero() && a.cfg.Now().Sub(attempted) < time.Hour {
		return nil
	}
	return a.Refresh(ctx)
}

// Refresh replaces this device's key. The old key is kept until the new one is seen working,
// so a failed refresh leaves the device exactly as it was.
func (a *Agent) Refresh(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}
	a.opMu.Lock()
	defer a.opMu.Unlock()
	a.mu.Lock()
	a.attempted = a.cfg.Now()
	a.mu.Unlock()

	priv, pub, err := wg.GenerateKeypair()
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.id.PendingPrivateKey, a.id.PendingPublicKey = priv, pub
	err = a.saveLocked()
	auth := a.id.AuthToken
	a.mu.Unlock()
	if err != nil {
		return err
	}

	if err := a.cfg.Control.Refresh(ctx, auth, pub); err != nil {
		a.mu.Lock()
		a.id.PendingPrivateKey, a.id.PendingPublicKey = "", ""
		a.prob = ProblemRefreshFailed
		a.saveLocked()
		a.mu.Unlock()
		return fmt.Errorf("could not refresh security: %w", err)
	}

	// The control plane accepts the new key now. Switch, and check the new key really connects.
	a.mu.Lock()
	a.id.PreviousPrivateKey, a.id.PreviousPublicKey = a.id.PrivateKey, a.id.PublicKey
	a.id.PrivateKey, a.id.PublicKey = priv, pub
	a.id.PendingPrivateKey, a.id.PendingPublicKey = "", ""
	err = a.saveLocked()
	last := a.last
	a.mu.Unlock()
	if err != nil {
		return err
	}
	switched := a.cfg.Now()
	if last != nil {
		if _, err := a.apply(ctx, *last); err != nil {
			a.cfg.Log.Warn("could not switch to the new security key", "error", err.Error())
		}
		if a.connectedSince(ctx, last.ServerPublicKey, switched) {
			a.mu.Lock()
			a.id.PreviousPrivateKey, a.id.PreviousPublicKey = "", ""
			a.prob = ProblemNone
			err := a.saveLocked()
			a.mu.Unlock()
			a.cfg.Log.Info("security refreshed")
			a.catchUp(ctx)
			return err
		}
	}
	return a.rollback(ctx, last)
}

// connectedSince waits for the server to show a handshake after the switch.
func (a *Agent) connectedSince(ctx context.Context, serverKey string, since time.Time) bool {
	deadline := time.Now().Add(a.cfg.HandshakeWait)
	for {
		peers, err := a.cfg.Backend.Peers(ctx, a.cfg.Iface)
		if err == nil {
			for _, p := range peers {
				if p.PublicKey == serverKey && !p.LastHandshake.IsZero() && !p.LastHandshake.Before(since) {
					return true
				}
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// rollback puts the old key back, both here and at the control plane.
func (a *Agent) rollback(ctx context.Context, last *DesiredState) error {
	a.mu.Lock()
	auth := a.id.AuthToken
	a.mu.Unlock()
	if err := a.cfg.Control.Rollback(ctx, auth); err != nil {
		// Not fatal: the next instructions say which key the control plane accepts and the agent follows.
		a.cfg.Log.Warn("could not tell the control plane to go back to the old key", "error", err.Error())
	}
	a.mu.Lock()
	if a.id.PreviousPrivateKey != "" {
		a.id.PrivateKey, a.id.PublicKey = a.id.PreviousPrivateKey, a.id.PreviousPublicKey
		a.id.PreviousPrivateKey, a.id.PreviousPublicKey = "", ""
	}
	a.prob = ProblemRefreshFailed
	a.saveLocked()
	a.mu.Unlock()
	if last != nil {
		if _, err := a.apply(ctx, *last); err != nil {
			return err
		}
	}
	a.cfg.Log.Warn("the new security key did not connect; went back to the old one")
	return ErrRefreshFailed
}

// catchUp takes the control plane's latest instructions after a refresh, so the version moves forward.
func (a *Agent) catchUp(ctx context.Context) {
	id := a.identity()
	s, changed, err := a.cfg.Control.Desired(ctx, id.AuthToken, -1)
	if err != nil || !changed {
		return
	}
	st, err := Verify(id.ControlKey, s)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if st.Version >= a.id.LastVersion {
		a.id.LastVersion = st.Version
		cp := st
		if a.last != nil {
			cp.Endpoint = a.last.Endpoint
		}
		a.last = &cp
		a.saveLocked()
	}
}

// ---- health ----

// ReportNow tells the control plane how this device sees its own connection.
func (a *Agent) ReportNow(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}
	r, _ := a.measure(ctx)
	a.mu.Lock()
	auth := a.id.AuthToken
	a.mu.Unlock()
	return a.cfg.Control.Report(ctx, auth, r)
}

// measure reads how the tunnel is doing right now. It also returns the last instructions applied, if any.
func (a *Agent) measure(ctx context.Context) (Report, *DesiredState) {
	a.mu.Lock()
	id, last, prob, appliedAt := a.id, a.last, a.prob, a.appliedAt
	a.mu.Unlock()
	now := a.cfg.Now()
	r := Report{DeviceID: id.DeviceID, Time: now.UTC(), AppliedVersion: id.LastVersion, AgentVersion: a.cfg.Version}
	if last != nil {
		peers, err := a.cfg.Backend.Peers(ctx, a.cfg.Iface)
		if err == nil {
			for _, p := range peers {
				if p.PublicKey == last.ServerPublicKey {
					r.LastHandshake, r.RxBytes, r.TxBytes = p.LastHandshake.UTC(), p.RxBytes, p.TxBytes
				}
			}
		}
		r.TunnelUp = !r.LastHandshake.IsZero() && now.Sub(r.LastHandshake) <= connectedWindow
		if prob == ProblemNone && !r.TunnelUp {
			stale := now.Sub(appliedAt) > firstConnectWait
			if !r.LastHandshake.IsZero() {
				stale = now.Sub(r.LastHandshake) > unreachableAfter
			}
			if stale {
				prob = ProblemServerUnreachable
			}
		}
	}
	r.Problem = prob
	return r, last
}

// ---- running ----

// teardown removes the tunnel and forgets the keys. It is what happens when this device is removed.
func (a *Agent) teardown(ctx context.Context) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if err := a.cfg.Backend.Remove(ctx, a.cfg.Iface); err != nil {
		a.cfg.Log.Warn("could not remove the tunnel", "error", err.Error())
	}
	os.Remove(a.idPath())
	a.mu.Lock()
	a.id, a.started, a.last = Identity{}, false, nil
	a.mu.Unlock()
}

// Run keeps this computer in step with the control plane until ctx ends or the device is removed.
func (a *Agent) Run(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}
	defer a.WriteStoppedStatus() // runs after cancel() below, so the ticks have stopped writing
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var revoked sync.Once
	gone := make(chan struct{})
	markRevoked := func() { revoked.Do(func() { close(gone); cancel() }) }

	tick := func(every time.Duration, f func()) {
		go func() {
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-t.C:
					f()
				}
			}
		}()
	}
	tick(a.cfg.ReportEvery, func() {
		if err := a.ReportNow(runCtx); errors.Is(err, ErrRevoked) {
			markRevoked()
		} else if err != nil && runCtx.Err() == nil {
			a.cfg.Log.Warn("could not report to the control plane", "error", err.Error())
		}
	})
	if a.cfg.StatusPath != "" {
		a.WriteStatus(runCtx)
		tick(5*time.Second, func() { a.WriteStatus(runCtx) })
	}
	tick(a.cfg.ResolveEvery, func() {
		if _, err := a.ResolveNow(runCtx); err != nil && runCtx.Err() == nil {
			a.cfg.Log.Warn("could not look up the server address", "error", err.Error())
		}
	})
	tick(a.cfg.RefreshCheckEvery, func() {
		for _, f := range []func(context.Context) error{a.RefreshIfRequested, a.RefreshIfDue} {
			if err := f(runCtx); err != nil && runCtx.Err() == nil {
				a.cfg.Log.Warn("security refresh", "error", err.Error())
			}
		}
	})

	since := int64(-1)
	backoff := time.Second
	for runCtx.Err() == nil {
		_, err := a.sync(runCtx, since)
		if errors.Is(err, ErrRevoked) {
			markRevoked()
			break
		}
		if err != nil {
			if runCtx.Err() != nil {
				break
			}
			if !errors.Is(err, gateway.ErrStaleState) {
				a.cfg.Log.Warn("could not apply instructions from the control plane", "error", err.Error())
			}
			select {
			case <-runCtx.Done():
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		since = a.identity().LastVersion
		_ = a.ReportNow(runCtx)
	}
	select {
	case <-gone:
		a.teardown(context.Background())
		a.cfg.Log.Info("this device was removed from the network; the tunnel and its keys are gone")
		return ErrRevoked
	default:
	}
	return ctx.Err()
}

// ---- refresh on request ----

const refreshTrigger = "refresh-now"

// RequestRefresh asks the agent running with this state folder to refresh its security soon.
func RequestRefresh(stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, refreshTrigger), []byte("now\n"), 0o600)
}

// RefreshIfRequested refreshes once if RequestRefresh was called since the last time.
func (a *Agent) RefreshIfRequested(ctx context.Context) error {
	p := filepath.Join(a.cfg.StateDir, refreshTrigger)
	if _, err := os.Stat(p); err != nil {
		return nil
	}
	os.Remove(p)
	return a.Refresh(ctx)
}
