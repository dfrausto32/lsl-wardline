package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Status is what the tray icon and the desktop window show. The service writes it to a file that every
// user of the computer can read, so it holds nothing secret: no keys, no access token.
type Status struct {
	Time          time.Time  `json:"time"` // when the service wrote it
	Version       string     `json:"version"`
	Interface     string     `json:"interface"`
	ControlURL    string     `json:"control_url"`
	DeviceID      string     `json:"device_id"`
	Address       string     `json:"address"` // this computer's address on the network, with the network size
	Server        string     `json:"server"`  // where the server is, as the server's name or address and port
	Connected     bool       `json:"connected"`
	LastHandshake time.Time  `json:"last_handshake"`
	RxBytes       int64      `json:"rx_bytes"`
	TxBytes       int64      `json:"tx_bytes"`
	Problem       string     `json:"problem,omitempty"`
	RefreshDue    *time.Time `json:"refresh_due,omitempty"`
}

type State string

const (
	StateConnected  State = "connected"
	StateConnecting State = "connecting"
	StateProblem    State = "problem"
	StateOff        State = "off"
)

// statusStaleAfter is how long after the last write the service is taken to have stopped.
const statusStaleAfter = 20 * time.Second

// Describe says in plain words how things are. A status that has not been refreshed lately means the
// service is not running.
func (s Status) Describe(now time.Time) (State, string) {
	if s.Time.IsZero() || now.Sub(s.Time) > statusStaleAfter {
		return StateOff, "Wardline is not running on this computer."
	}
	switch s.Problem {
	case ProblemServerUnreachable:
		return StateProblem, "Can't reach the server. Check this computer's internet connection."
	case ProblemNameNotFound:
		return StateProblem, "Can't find the server's address. Check this computer's internet connection."
	case ProblemKeyMismatch:
		return StateProblem, "This computer's security does not match the server's. Wardline is fixing it."
	case ProblemRefreshFailed:
		return StateProblem, "Wardline could not refresh this computer's security, so it is still using the old one."
	}
	if s.Connected {
		return StateConnected, "Connected"
	}
	return StateConnecting, "Connecting…"
}

// WriteStatus writes the status file, if one was asked for. It is safe to call often.
func (a *Agent) WriteStatus(ctx context.Context) error {
	if a.cfg.StatusPath == "" {
		return nil
	}
	r, last := a.measure(ctx)
	s := Status{
		Time: a.cfg.Now().UTC(), Version: a.cfg.Version, Interface: a.cfg.Iface, ControlURL: a.cfg.ControlURL,
		DeviceID: r.DeviceID, Connected: r.TunnelUp, LastHandshake: r.LastHandshake,
		RxBytes: r.RxBytes, TxBytes: r.TxBytes, Problem: r.Problem,
	}
	if last != nil {
		s.Address, s.Server, s.RefreshDue = last.Address, last.Endpoint, last.RefreshDue
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(a.cfg.StatusPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "status-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	os.Chmod(tmp.Name(), 0o644) // anyone using this computer may read it
	if err := os.Rename(tmp.Name(), a.cfg.StatusPath); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// LoadStatus reads the status file. A missing or damaged file gives an empty status, which Describe
// reports as "not running".
func LoadStatus(path string) (Status, error) {
	s, err := ReadStatus(path)
	if err != nil {
		return Status{}, err
	}
	return s, nil
}

// ReadStatus reads the status file and reports problems with it.
func ReadStatus(path string) (Status, error) {
	var s Status
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}
