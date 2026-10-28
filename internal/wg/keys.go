package wg

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateKeypair returns a clamped Curve25519 keypair, base64 encoded.
func GenerateKeypair() (private, public string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	b[0] &= 248
	b[31] = (b[31] & 127) | 64
	private = base64.StdEncoding.EncodeToString(b[:])
	public, err = PublicFromPrivate(private)
	return private, public, err
}

// PublicFromPrivate derives the public key.
func PublicFromPrivate(private string) (string, error) {
	raw, err := decodeKey(private)
	if err != nil {
		return "", err
	}
	k, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// GeneratePresharedKey returns 32 random bytes, base64 encoded.
func GeneratePresharedKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b[:]), nil
}

func decodeKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("not a valid key")
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("not a valid key")
	}
	return b, nil
}

// ValidateKey checks that s is a base64 encoded 32 byte value.
func ValidateKey(s string) error {
	_, err := decodeKey(s)
	return err
}
