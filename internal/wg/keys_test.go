package wg

import (
	"crypto/ecdh"
	"encoding/base64"
	"testing"
)

func mustKey(t *testing.T) string {
	t.Helper()
	priv, _, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func mustPub(t *testing.T) string {
	t.Helper()
	_, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestGenerateKeypairShapeAndUniqueness(t *testing.T) {
	priv, pub, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{priv, pub} {
		b, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(b) != 32 {
			t.Fatalf("bad key %q: %v", k, err)
		}
	}
	priv2, _, _ := GenerateKeypair()
	if priv == priv2 {
		t.Fatal("keys must differ")
	}
	// Curve25519 clamping, as WireGuard requires.
	b, _ := base64.StdEncoding.DecodeString(priv)
	if b[0]&7 != 0 || b[31]&128 != 0 || b[31]&64 == 0 {
		t.Fatal("private key is not clamped")
	}
}

func TestPublicFromPrivateMatchesTheStandardLibrary(t *testing.T) {
	// The expected public key is worked out here, by Go's own X25519, so no key is written in the source.
	for i := 0; i < 5; i++ {
		priv := mustKey(t)
		raw, _ := base64.StdEncoding.DecodeString(priv)
		k, err := ecdh.X25519().NewPrivateKey(raw)
		if err != nil {
			t.Fatal(err)
		}
		want := base64.StdEncoding.EncodeToString(k.PublicKey().Bytes())
		got, err := PublicFromPrivate(priv)
		if err != nil || got != want {
			t.Fatalf("got %q want %q err %v", got, want, err)
		}
	}
}

func TestPublicFromPrivateRoundTrip(t *testing.T) {
	priv, pub, _ := GenerateKeypair()
	got, err := PublicFromPrivate(priv)
	if err != nil || got != pub {
		t.Fatalf("%q %q %v", got, pub, err)
	}
}

func TestValidateKey(t *testing.T) {
	if err := ValidateKey("not a key"); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidateKey(mustPub(t)); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratePresharedKey(t *testing.T) {
	k, err := GeneratePresharedKey()
	if err != nil || ValidateKey(k) != nil {
		t.Fatalf("%q %v", k, err)
	}
}
