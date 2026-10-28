// Package agent is the device role: a small program on a computer that makes
// its own key, asks the control plane how to connect, and keeps the tunnel
// matching. This file holds what the control plane and the agent both need.
// The private key never leaves the computer; the control plane sees only the
// public half.
package agent

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"time"

	"lsl-wardline/internal/gateway"
)

var ErrBadSignature = errors.New("the instructions from the control plane could not be verified")

// EnrollRequest is sent once, with the one-time code from the install command.
type EnrollRequest struct {
	Token        string `json:"token"`
	PublicKey    string `json:"public_key"`
	AgentVersion string `json:"agent_version,omitempty"`
}

// EnrollResponse gives the agent its identity and the key that signs its instructions.
type EnrollResponse struct {
	DeviceID   string            `json:"device_id"`
	AuthToken  string            `json:"auth_token"`
	ControlKey ed25519.PublicKey `json:"control_key"`
}

// DesiredState is everything an agent needs to build its tunnel. Version only goes up.
type DesiredState struct {
	Version         int64     `json:"version"`
	IssuedAt        time.Time `json:"issued_at"`
	DeviceID        string    `json:"device_id"`
	Address         string    `json:"address"`           // this device's tunnel address with the network prefix, e.g. 10.77.5.2/24
	ServerPublicKey string    `json:"server_public_key"` // the server to connect to
	Endpoint        string    `json:"endpoint"`          // host:port of the server
	AllowedIPs      []string  `json:"allowed_ips"`
	PresharedKey    string    `json:"preshared_key,omitempty"`
	Keepalive       int       `json:"keepalive"`
}

// Sign signs the state with the control plane's key. It uses the same envelope as the server role.
func Sign(priv ed25519.PrivateKey, st DesiredState) (gateway.Signed, error) {
	b, err := json.Marshal(st)
	if err != nil {
		return gateway.Signed{}, err
	}
	return gateway.Signed{Payload: b, Signature: ed25519.Sign(priv, b)}, nil
}

// Verify checks the signature and decodes the state.
func Verify(pub ed25519.PublicKey, s gateway.Signed) (DesiredState, error) {
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, s.Payload, s.Signature) {
		return DesiredState{}, ErrBadSignature
	}
	var st DesiredState
	if err := json.Unmarshal(s.Payload, &st); err != nil {
		return DesiredState{}, err
	}
	return st, nil
}
