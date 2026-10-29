// Package gateway is the server role: it enrolls with the control plane,
// pulls signed desired state, applies it idempotently and reports who is
// connected. The server's private key never leaves this machine.
package gateway

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrBadSignature = errors.New("the instructions from the control plane could not be verified")
	ErrStaleState   = errors.New("the control plane sent instructions older than the ones already applied")
)

// DesiredPeer is one device the server must accept.
type DesiredPeer struct {
	DeviceID     string `json:"device_id"`
	PublicKey    string `json:"public_key"`
	PresharedKey string `json:"preshared_key,omitempty"`
	TunnelIP     string `json:"tunnel_ip"`
	Keepalive    int    `json:"keepalive,omitempty"`
}

// DesiredState is everything a server needs to know. Version only goes up.
type DesiredState struct {
	Version    int64         `json:"version"`
	IssuedAt   time.Time     `json:"issued_at"`
	ServerID   string        `json:"server_id"`
	Address    string        `json:"address"` // the server's own address with the network prefix, e.g. 10.77.5.1/24
	ListenPort int           `json:"listen_port"`
	Peers      []DesiredPeer `json:"peers"`
	// DevicesReachEachOther is the network setting: when true the server relays traffic from one device to another.
	DevicesReachEachOther bool `json:"devices_reach_each_other"`
}

// Signed carries the exact bytes that were signed.
type Signed struct {
	Payload   []byte `json:"payload"`
	Signature []byte `json:"signature"`
}

// Sign signs the state with the control plane's key.
func Sign(priv ed25519.PrivateKey, st DesiredState) (Signed, error) {
	b, err := json.Marshal(st)
	if err != nil {
		return Signed{}, err
	}
	return Signed{Payload: b, Signature: ed25519.Sign(priv, b)}, nil
}

// Verify checks the signature and decodes the state.
func Verify(pub ed25519.PublicKey, s Signed) (DesiredState, error) {
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, s.Payload, s.Signature) {
		return DesiredState{}, ErrBadSignature
	}
	var st DesiredState
	if err := json.Unmarshal(s.Payload, &st); err != nil {
		return DesiredState{}, err
	}
	return st, nil
}

// EnrollRequest is sent once, with the one-time token from the install command.
type EnrollRequest struct {
	Token     string `json:"token"`
	PublicKey string `json:"public_key"`
	Name      string `json:"name"`
}

// EnrollResponse gives the server its identity and the key that signs its instructions.
type EnrollResponse struct {
	ServerID   string            `json:"server_id"`
	AuthToken  string            `json:"auth_token"`
	ControlKey ed25519.PublicKey `json:"control_key"`
}

// PeerReport is what the server saw for one device.
type PeerReport struct {
	DeviceID      string    `json:"device_id"`
	PublicKey     string    `json:"public_key"`
	LastHandshake time.Time `json:"last_handshake"`
	RxBytes       int64     `json:"rx_bytes"`
	TxBytes       int64     `json:"tx_bytes"`
	Endpoint      string    `json:"endpoint,omitempty"`
}

// Report is sent regularly so the web app can show who is connected.
type Report struct {
	ServerID string       `json:"server_id"`
	Time     time.Time    `json:"time"`
	Applied  int64        `json:"applied_version"`
	Peers    []PeerReport `json:"peers"`
}
