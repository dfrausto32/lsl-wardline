package auth

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lsl-wardline/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---- passwords ----

func TestPasswordHashIsArgon2idAndVerifies(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("not argon2id: %s", h)
	}
	if !VerifyPassword(h, "correct horse battery") || VerifyPassword(h, "wrong") {
		t.Fatal("verify")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("salts must differ")
	}
	if VerifyPassword("garbage", "x") || VerifyPassword("", "") {
		t.Fatal("malformed hash must not verify")
	}
}

func TestPasswordPolicyUsesPlainLanguage(t *testing.T) {
	if err := ValidatePassword("short"); err == nil || !strings.Contains(err.Error(), "10") {
		t.Fatalf("got %v", err)
	}
	if err := ValidatePassword("a long enough passphrase"); err != nil {
		t.Fatal(err)
	}
}

// ---- TOTP ----

func TestTOTPMatchesRFC6238Vector(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	// RFC 6238 appendix B: T=59 gives 94287082 with 8 digits, so 287082 with 6.
	if got := TOTPCode(secret, time.Unix(59, 0)); got != "287082" {
		t.Fatalf("got %s", got)
	}
	if got := TOTPCode(secret, time.Unix(1111111109, 0)); got != "081804" {
		t.Fatalf("got %s", got)
	}
}

func TestTOTPValidationWindow(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	if !ValidTOTP(secret, TOTPCode(secret, now), now) {
		t.Fatal("current code")
	}
	if !ValidTOTP(secret, TOTPCode(secret, now.Add(-30*time.Second)), now) {
		t.Fatal("previous step should be accepted for clock drift")
	}
	if ValidTOTP(secret, TOTPCode(secret, now.Add(-90*time.Second)), now) {
		t.Fatal("old code accepted")
	}
	if ValidTOTP(secret, "12345", now) || ValidTOTP(secret, "abcdef", now) || ValidTOTP("!!!", "123456", now) {
		t.Fatal("malformed input accepted")
	}
}

func TestTOTPURLNamesTheApp(t *testing.T) {
	u := TOTPURL("JBSWY3DPEHPK3PXP", "maria@example.com")
	if !strings.HasPrefix(u, "otpauth://totp/Wardline:") || !strings.Contains(u, "secret=JBSWY3DPEHPK3PXP") || !strings.Contains(u, "issuer=Wardline") {
		t.Fatal(u)
	}
}

// ---- encryption at rest ----

func TestSecretBoxRoundTripAndTamper(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ct1, n1, err := box.Seal([]byte("psk-value"))
	if err != nil {
		t.Fatal(err)
	}
	ct2, n2, _ := box.Seal([]byte("psk-value"))
	if bytes.Equal(n1, n2) || bytes.Equal(ct1, ct2) {
		t.Fatal("nonce reuse")
	}
	if bytes.Contains(ct1, []byte("psk-value")) {
		t.Fatal("not encrypted")
	}
	pt, err := box.Open(ct1, n1)
	if err != nil || string(pt) != "psk-value" {
		t.Fatalf("%q %v", pt, err)
	}
	ct1[0] ^= 1
	if _, err := box.Open(ct1, n1); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	other, _ := NewSecretBox(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(ct2, n2); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := NewSecretBox([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestSecretBoxPackedForm(t *testing.T) {
	box, _ := NewSecretBox(bytes.Repeat([]byte{1}, 32))
	packed, err := box.SealPacked([]byte("totp-secret"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := box.OpenPacked(packed)
	if err != nil || string(got) != "totp-secret" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := box.OpenPacked([]byte{1, 2, 3}); err == nil {
		t.Fatal("short blob accepted")
	}
}

func TestLoadKeyFromFileIsCreatedOnceWithPrivatePermissions(t *testing.T) {
	t.Setenv("WARDLINE_SECRET_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	k1, err := LoadOrCreateKey(path)
	if err != nil || len(k1) != 32 {
		t.Fatalf("%v %d", err, len(k1))
	}
	k2, err := LoadOrCreateKey(path)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatal("key changed between loads")
	}
	if fi, _ := os.Stat(path); os.PathSeparator == '/' && fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("key file mode %v", fi.Mode())
	}
}

func TestLoadKeyPrefersEnvironment(t *testing.T) {
	want := bytes.Repeat([]byte{9}, 32)
	t.Setenv("WARDLINE_SECRET_KEY", base64.StdEncoding.EncodeToString(want))
	got, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "never-created.key"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%v", err)
	}
	t.Setenv("WARDLINE_SECRET_KEY", "not base64 of 32 bytes")
	if _, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "x.key")); err == nil {
		t.Fatal("bad env key accepted")
	}
}

// ---- admin accounts ----

func TestCreateAdminOnlyOnceAndNoDefaults(t *testing.T) {
	s := newStore(t)
	a := NewAccounts(s.DB(), nil)
	ctx := context.Background()
	if _, err := a.Authenticate(ctx, "admin", "admin", ""); err == nil {
		t.Fatal("default credentials must not exist")
	}
	if n, _ := a.Count(ctx); n != 0 {
		t.Fatal("fresh database must have no admin")
	}
	id, err := a.Create(ctx, s.DB(), "Maria@Example.com", "a long enough passphrase")
	if err != nil || id == "" {
		t.Fatal(err)
	}
	if _, err := a.Create(ctx, s.DB(), "other@example.com", "a long enough passphrase"); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("second admin: %v", err)
	}
	if _, err := a.Create(ctx, s.DB(), "not-an-email", "a long enough passphrase"); err == nil {
		t.Fatal("bad email accepted")
	}
}

func TestAuthenticateGivesTheSameErrorForWrongPasswordAndUnknownEmail(t *testing.T) {
	s := newStore(t)
	a := NewAccounts(s.DB(), nil)
	ctx := context.Background()
	a.Create(ctx, s.DB(), "maria@example.com", "a long enough passphrase")
	_, e1 := a.Authenticate(ctx, "maria@example.com", "wrong password here", "")
	_, e2 := a.Authenticate(ctx, "nobody@example.com", "wrong password here", "")
	if !errors.Is(e1, ErrBadLogin) || !errors.Is(e2, ErrBadLogin) || e1.Error() != e2.Error() {
		t.Fatalf("%v / %v", e1, e2)
	}
	id, err := a.Authenticate(ctx, "MARIA@example.com", "a long enough passphrase", "")
	if err != nil || id == "" {
		t.Fatalf("case-insensitive email: %v", err)
	}
}

func TestTOTPIsRequiredOnceEnabledAndStoredEncrypted(t *testing.T) {
	s := newStore(t)
	box, _ := NewSecretBox(bytes.Repeat([]byte{3}, 32))
	a := NewAccounts(s.DB(), box)
	ctx := context.Background()
	id, _ := a.Create(ctx, s.DB(), "maria@example.com", "a long enough passphrase")
	secret, _ := NewTOTPSecret()
	now := time.Now()
	if err := a.EnableTOTP(ctx, id, secret, TOTPCode(secret, now), now); err != nil {
		t.Fatal(err)
	}
	var blob []byte
	s.DB().QueryRow(`SELECT totp_secret_ciphertext FROM admin_users WHERE id=?`, id).Scan(&blob)
	if len(blob) == 0 || bytes.Contains(blob, []byte(secret)) {
		t.Fatal("secret must be stored encrypted")
	}
	if _, err := a.Authenticate(ctx, "maria@example.com", "a long enough passphrase", ""); !errors.Is(err, ErrNeedCode) {
		t.Fatalf("want ErrNeedCode, got %v", err)
	}
	if _, err := a.Authenticate(ctx, "maria@example.com", "a long enough passphrase", "000000"); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("wrong code: %v", err)
	}
	if _, err := a.AuthenticateAt(ctx, "maria@example.com", "a long enough passphrase", TOTPCode(secret, now), now); err != nil {
		t.Fatal(err)
	}
	if err := a.EnableTOTP(ctx, id, secret, "000000", now); err == nil {
		t.Fatal("enabling with a wrong code must fail")
	}
}

// ---- sessions ----

func TestSessionsStoreOnlyAHashAndExpire(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a := NewAccounts(s.DB(), nil)
	id, _ := a.Create(ctx, s.DB(), "maria@example.com", "a long enough passphrase")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ss := &Sessions{DB: s.DB(), TTL: time.Hour, Now: func() time.Time { return now }}

	tok, err := ss.Create(ctx, id)
	if err != nil || len(tok) < 32 {
		t.Fatalf("%q %v", tok, err)
	}
	var n int
	s.DB().QueryRow(`SELECT count(*) FROM sessions WHERE token_hash=?`, tok).Scan(&n)
	if n != 0 {
		t.Fatal("raw token stored")
	}
	got, err := ss.Lookup(ctx, tok)
	if err != nil || got != id {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ss.Lookup(ctx, tok+"x"); err == nil {
		t.Fatal("wrong token accepted")
	}
	now = now.Add(61 * time.Minute)
	if _, err := ss.Lookup(ctx, tok); !errors.Is(err, ErrNoSession) {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestSessionDeleteSignsOut(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a := NewAccounts(s.DB(), nil)
	id, _ := a.Create(ctx, s.DB(), "maria@example.com", "a long enough passphrase")
	ss := &Sessions{DB: s.DB(), TTL: time.Hour}
	tok, _ := ss.Create(ctx, id)
	ss.Delete(ctx, tok)
	if _, err := ss.Lookup(ctx, tok); err == nil {
		t.Fatal("still signed in")
	}
}

// ---- rate limiting ----

func TestLimiterBlocksAfterRepeatedFailuresAndRecovers(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &Limiter{Max: 3, Window: 10 * time.Minute, Now: func() time.Time { return now }}
	key := "1.2.3.4|maria@example.com"
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(key); !ok {
			t.Fatalf("blocked too early at %d", i)
		}
		l.Fail(key)
	}
	ok, wait := l.Allow(key)
	if ok || wait <= 0 || wait > 10*time.Minute {
		t.Fatalf("ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("other keys must be unaffected")
	}
	now = now.Add(11 * time.Minute)
	if ok, _ := l.Allow(key); !ok {
		t.Fatal("should recover after the window")
	}
}

func TestLimiterResetsOnSuccess(t *testing.T) {
	l := &Limiter{Max: 2, Window: time.Minute}
	l.Fail("k")
	l.Succeed("k")
	l.Fail("k")
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("success should reset the count")
	}
}
