package wg

import (
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

func TestPublicFromPrivateMatchesKnownVector(t *testing.T) {
	// Expected value produced by the reference tool: `echo <priv> | wg pubkey`
	// (wireguard-tools in the dev container).
	priv := "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="
	want := "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw="
	got, err := PublicFromPrivate(priv)
	if err != nil || got != want {
		t.Fatalf("got %q err %v", got, err)
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
