package wg

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WireGuardApp is a Backend that drives the official WireGuard for Windows program instead of
// carrying its own tunnel code: it creates the tunnel as a Windows service with wireguard.exe and
// changes it while running with wg.exe. It is meant for Windows computers. Only the pieces that do
// not need Windows (the config text, reading wg's output, the order of the commands) are tested in
// the container; running it for real is a manual check (see specs/ACCEPTANCE.md, M6).
//
// Keys are never put on a command line: they go in config files in a folder only the system and
// administrators can read, and wg.exe reads them from there.
type WireGuardApp struct {
	WireGuardExe string
	WgExe        string
	Dir          string // where tunnel configs are kept

	// Run, WriteFile and RemoveFile can be replaced in tests. The defaults run the real programs.
	Run        func(ctx context.Context, exe string, args ...string) ([]byte, error)
	WriteFile  func(path string, data []byte) error
	RemoveFile func(path string) error

	mu   sync.Mutex
	cfgs map[string]InterfaceConfig // what this process last configured
}

// NewWireGuardApp returns the backend with the usual install locations.
func NewWireGuardApp(dir string) *WireGuardApp {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return &WireGuardApp{
		WireGuardExe: filepath.Join(pf, "WireGuard", "wireguard.exe"),
		WgExe:        filepath.Join(pf, "WireGuard", "wg.exe"),
		Dir:          dir,
	}
}

// Installed says whether the official WireGuard for Windows is on this computer.
func (a *WireGuardApp) Installed() bool {
	for _, p := range []string{a.WireGuardExe, a.WgExe} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func (a *WireGuardApp) run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	if a.Run != nil {
		return a.Run(ctx, exe, args...)
	}
	out, err := exec.CommandContext(ctx, exe, args...).CombinedOutput()
	if err != nil {
		// "wg show ... dump" prints the private key, so its output is never put in an error.
		if len(args) > 0 && args[0] == "show" {
			return nil, fmt.Errorf("%s %s failed: %w", filepath.Base(exe), args[0], err)
		}
		return out, fmt.Errorf("%s %s failed: %w: %s", filepath.Base(exe), strings.Join(args[:min(len(args), 1)], " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (a *WireGuardApp) write(path string, data []byte) error {
	if a.WriteFile != nil {
		return a.WriteFile(path, data)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (a *WireGuardApp) remove(path string) error {
	if a.RemoveFile != nil {
		return a.RemoveFile(path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *WireGuardApp) confPath(name string) string { return filepath.Join(a.Dir, name+".conf") }
func (a *WireGuardApp) syncPath(name string) string { return filepath.Join(a.Dir, name+".sync.conf") }

// ---- reading and writing WireGuard text ----

// parseDump reads the output of "wg show <name> dump": one line for the interface, then one per peer.
func parseDump(s string) ([]PeerState, error) {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 || len(strings.Split(lines[0], "\t")) < 4 {
		return nil, errors.New("unexpected output from wg")
	}
	var out []PeerState
	for _, line := range lines[1:] {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 8 {
			return nil, errors.New("unexpected peer line in the output from wg")
		}
		p := PeerState{}
		p.PublicKey = f[0]
		if f[1] != "(none)" {
			p.PresharedKey = f[1]
		}
		if f[2] != "(none)" {
			p.Endpoint, p.LastEndpoint = f[2], f[2]
		}
		if f[3] != "(none)" {
			for _, s := range strings.Split(f[3], ",") {
				if pr, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil {
					p.AllowedIPs = append(p.AllowedIPs, pr)
				}
			}
		}
		if n, err := strconv.ParseInt(f[4], 10, 64); err == nil && n > 0 {
			p.LastHandshake = time.Unix(n, 0)
		}
		p.RxBytes, _ = strconv.ParseInt(f[5], 10, 64)
		p.TxBytes, _ = strconv.ParseInt(f[6], 10, 64)
		if f[7] != "off" {
			p.Keepalive, _ = strconv.Atoi(f[7])
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublicKey < out[j].PublicKey })
	return out, nil
}

// renderConf writes a config. The tunnel service needs the Address line; "wg syncconf" does not accept it.
func renderConf(cfg InterfaceConfig, peers []PeerConfig, forService bool) string {
	var b strings.Builder
	b.WriteString("[Interface]\nPrivateKey = " + cfg.PrivateKey + "\n")
	if cfg.ListenPort != 0 {
		b.WriteString("ListenPort = " + strconv.Itoa(cfg.ListenPort) + "\n")
	}
	if forService {
		b.WriteString("Address = " + cfg.Address.String() + "\n")
	}
	for _, p := range peers {
		b.WriteString("\n[Peer]\nPublicKey = " + p.PublicKey + "\n")
		if p.PresharedKey != "" {
			b.WriteString("PresharedKey = " + p.PresharedKey + "\n")
		}
		var ips []string
		for _, ip := range sortedIPs(p.AllowedIPs) {
			ips = append(ips, ip.String())
		}
		b.WriteString("AllowedIPs = " + strings.Join(ips, ", ") + "\n")
		if p.Endpoint != "" {
			b.WriteString("Endpoint = " + p.Endpoint + "\n")
		}
		if p.Keepalive > 0 {
			b.WriteString("PersistentKeepalive = " + strconv.Itoa(p.Keepalive) + "\n")
		}
	}
	return b.String()
}

// ---- Backend ----

func (a *WireGuardApp) live(ctx context.Context, name string) ([]PeerState, error) {
	out, err := a.run(ctx, a.WgExe, "show", name, "dump")
	if err != nil {
		return nil, err
	}
	return parseDump(string(out))
}

func peerConfigs(in []PeerState) []PeerConfig {
	out := make([]PeerConfig, 0, len(in))
	for _, p := range in {
		out = append(out, p.PeerConfig)
	}
	return out
}

func (a *WireGuardApp) Configure(ctx context.Context, name string, cfg InterfaceConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfgs == nil {
		a.cfgs = map[string]InterfaceConfig{}
	}
	prev, known := a.cfgs[name]
	live, liveErr := a.live(ctx, name)
	a.cfgs[name] = cfg

	if liveErr == nil && known && prev.Address == cfg.Address {
		if prev.PrivateKey == cfg.PrivateKey && prev.ListenPort == cfg.ListenPort {
			return nil
		}
		return a.syncLocked(ctx, name, cfg, peerConfigs(live)) // a new key without dropping the tunnel
	}
	// Anything else starts the tunnel from a clean config, so nothing stale from before is left running.
	// The peers are put back straight afterwards by the caller.
	if liveErr == nil {
		if _, err := a.run(ctx, a.WireGuardExe, "/uninstalltunnelservice", name); err != nil {
			return err
		}
	}
	if err := a.write(a.confPath(name), []byte(renderConf(cfg, nil, true))); err != nil {
		return err
	}
	_, err := a.run(ctx, a.WireGuardExe, "/installtunnelservice", a.confPath(name))
	return err
}

// syncLocked makes the running tunnel match cfg and peers, and updates the saved config so a restart keeps it.
func (a *WireGuardApp) syncLocked(ctx context.Context, name string, cfg InterfaceConfig, peers []PeerConfig) error {
	if err := a.write(a.confPath(name), []byte(renderConf(cfg, peers, true))); err != nil {
		return err
	}
	tmp := a.syncPath(name)
	if err := a.write(tmp, []byte(renderConf(cfg, peers, false))); err != nil {
		return err
	}
	defer a.remove(tmp) // it holds keys
	_, err := a.run(ctx, a.WgExe, "syncconf", name, tmp)
	return err
}

func (a *WireGuardApp) Peers(ctx context.Context, name string) ([]PeerState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, err := a.live(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("interface %s does not exist", name)
	}
	return p, nil
}

func (a *WireGuardApp) ApplyDiff(ctx context.Context, name string, d Diff) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg, ok := a.cfgs[name]
	if !ok {
		return fmt.Errorf("interface %s has not been configured", name)
	}
	live, err := a.live(ctx, name)
	if err != nil {
		return fmt.Errorf("interface %s does not exist", name)
	}
	merged := map[string]PeerConfig{}
	for _, p := range live {
		merged[p.PublicKey] = p.PeerConfig
	}
	for _, k := range d.Remove {
		delete(merged, k)
	}
	for _, p := range append(append([]PeerConfig{}, d.Add...), d.Update...) {
		merged[p.PublicKey] = p
	}
	peers := make([]PeerConfig, 0, len(merged))
	for _, p := range merged {
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].PublicKey < peers[j].PublicKey })
	return a.syncLocked(ctx, name, cfg, peers)
}

func (a *WireGuardApp) Remove(ctx context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.cfgs, name)
	_, _ = a.run(ctx, a.WireGuardExe, "/uninstalltunnelservice", name)
	if err := a.remove(a.confPath(name)); err != nil {
		return err
	}
	if _, err := a.live(ctx, name); err == nil {
		return fmt.Errorf("the tunnel %s is still running", name)
	}
	return nil
}
