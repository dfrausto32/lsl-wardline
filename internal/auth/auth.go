// Package auth handles admin sign-in: argon2id passwords, optional TOTP,
// sessions, rate limiting, and encryption of secrets at rest.
package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"lsl-wardline/internal/store"
)

var (
	ErrBadLogin    = errors.New("That email and password do not match.")
	ErrNeedCode    = errors.New("Enter the 6-digit code from your authenticator app.")
	ErrAdminExists = errors.New("An administrator already exists for this network.")
	ErrNoSession   = errors.New("You are signed out.")
)

// ---- passwords ----

const (
	argonTime    = 2
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

// HashPassword returns a PHC-format argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether pw matches the PHC hash.
func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var v, m, t, p int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m <= 0 || t <= 0 || p <= 0 || m > 1<<20 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ValidatePassword enforces the password rules in plain language.
func ValidatePassword(pw string) error {
	if len([]rune(pw)) < 10 {
		return errors.New("Choose a password with at least 10 characters. A few words in a row works well.")
	}
	return nil
}

// ---- TOTP (RFC 6238, SHA-1, 30 second steps, 6 digits) ----

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

func totpAt(key []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%06d", code)
}

// TOTPCode returns the code valid at t.
func TOTPCode(secret string, t time.Time) string {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	return totpAt(key, uint64(t.Unix()/30))
}

// ValidTOTP accepts the current code and the one before or after it (clock drift).
func ValidTOTP(secret, code string, now time.Time) bool {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil || len(code) != 6 {
		return false
	}
	c := uint64(now.Unix() / 30)
	ok := false
	for _, d := range []int64{-1, 0, 1} {
		if subtle.ConstantTimeCompare([]byte(totpAt(key, uint64(int64(c)+d))), []byte(code)) == 1 {
			ok = true
		}
	}
	return ok
}

// TOTPURL is what authenticator apps read from a QR code.
func TOTPURL(secret, account string) string {
	return fmt.Sprintf("otpauth://totp/Wardline:%s?secret=%s&issuer=Wardline", url.PathEscape(account), secret)
}

// ---- encryption at rest ----

type SecretBox struct{ aead cipher.AEAD }

func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != 32 {
		return nil, errors.New("the secret key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: a}, nil
}

func (b *SecretBox) Seal(plain []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, b.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return b.aead.Seal(nil, nonce, plain, nil), nonce, nil
}

func (b *SecretBox) Open(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != b.aead.NonceSize() {
		return nil, errors.New("stored secret is damaged")
	}
	pt, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("stored secret could not be decrypted")
	}
	return pt, nil
}

// SealPacked returns nonce||ciphertext as one blob.
func (b *SecretBox) SealPacked(plain []byte) ([]byte, error) {
	ct, n, err := b.Seal(plain)
	if err != nil {
		return nil, err
	}
	return append(n, ct...), nil
}

func (b *SecretBox) OpenPacked(blob []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(blob) <= n {
		return nil, errors.New("stored secret is damaged")
	}
	return b.Open(blob[n:], blob[:n])
}

// LoadOrCreateKey returns the 32-byte secret key from WARDLINE_SECRET_KEY
// (base64) or from the key file, creating the file on first run. The key is
// never stored in the database.
func LoadOrCreateKey(path string) ([]byte, error) {
	if env := os.Getenv("WARDLINE_SECRET_KEY"); env != "" {
		k, err := base64.StdEncoding.DecodeString(env)
		if err != nil || len(k) != 32 {
			return nil, errors.New("WARDLINE_SECRET_KEY must be 32 bytes, base64 encoded")
		}
		return k, nil
	}
	if b, err := os.ReadFile(path); err == nil {
		k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(k) != 32 {
			return nil, fmt.Errorf("the key file %s is damaged", path)
		}
		return k, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(k)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

// ---- accounts ----

// Q is satisfied by *sql.DB and *sql.Tx.
type Q interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Accounts struct {
	db  *sql.DB
	box *SecretBox
}

func NewAccounts(db *sql.DB, box *SecretBox) *Accounts { return &Accounts{db: db, box: box} }

func (a *Accounts) Count(ctx context.Context) (int, error) {
	var n int
	err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM admin_users`).Scan(&n)
	return n, err
}

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// Create adds the one administrator. There are no default credentials.
func (a *Accounts) Create(ctx context.Context, q Q, email, password string) (string, error) {
	email = normEmail(email)
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") || len(email) < 3 {
		return "", errors.New("Enter a valid email address.")
	}
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM admin_users`).Scan(&n); err != nil {
		return "", err
	}
	if n > 0 {
		return "", ErrAdminExists
	}
	h, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	id := store.NewID()
	_, err = q.ExecContext(ctx, `INSERT INTO admin_users(id,email,password_hash,created_at) VALUES(?,?,?,?)`, id, email, h, store.Now())
	return id, err
}

// dummyHash makes unknown-email logins cost the same as wrong-password ones.
var dummyHash, _ = HashPassword("dummy password for timing")

func (a *Accounts) Authenticate(ctx context.Context, email, password, code string) (string, error) {
	return a.AuthenticateAt(ctx, email, password, code, time.Now())
}

func (a *Accounts) AuthenticateAt(ctx context.Context, email, password, code string, now time.Time) (string, error) {
	var id, hash string
	var totp []byte
	err := a.db.QueryRowContext(ctx, `SELECT id, password_hash, totp_secret_ciphertext FROM admin_users WHERE email=?`, normEmail(email)).Scan(&id, &hash, &totp)
	if err != nil {
		VerifyPassword(dummyHash, password)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrBadLogin
		}
		return "", err
	}
	if !VerifyPassword(hash, password) {
		return "", ErrBadLogin
	}
	if len(totp) > 0 {
		if code == "" {
			return "", ErrNeedCode
		}
		if a.box == nil {
			return "", errors.New("two-step sign-in is not available on this server")
		}
		secret, err := a.box.OpenPacked(totp)
		if err != nil {
			return "", err
		}
		if !ValidTOTP(string(secret), code, now) {
			return "", ErrBadLogin
		}
	}
	return id, nil
}

func (a *Accounts) HasTOTP(ctx context.Context, id string) (bool, error) {
	var n int
	err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM admin_users WHERE id=? AND totp_secret_ciphertext IS NOT NULL`, id).Scan(&n)
	return n > 0, err
}

// EnableTOTP stores the secret (encrypted) once the person proves their app works.
func (a *Accounts) EnableTOTP(ctx context.Context, id, secret, code string, now time.Time) error {
	if a.box == nil {
		return errors.New("two-step sign-in is not available on this server")
	}
	if !ValidTOTP(secret, code, now) {
		return errors.New("That code did not match. Check the 6-digit code in your authenticator app and try again.")
	}
	blob, err := a.box.SealPacked([]byte(secret))
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `UPDATE admin_users SET totp_secret_ciphertext=? WHERE id=?`, blob, id)
	return err
}

// ---- sessions ----

type Sessions struct {
	DB  *sql.DB
	TTL time.Duration
	Now func() time.Time
}

func (s *Sessions) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// Create returns the raw token to put in a cookie; only its hash is stored.
func (s *Sessions) Create(ctx context.Context, adminID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	ttl := s.TTL
	if ttl == 0 {
		ttl = 12 * time.Hour
	}
	now := s.now()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(id,admin_user_id,token_hash,created_at,expires_at) VALUES(?,?,?,?,?)`,
		store.NewID(), adminID, hashToken(tok), now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	return tok, err
}

// Lookup returns the admin id for a valid, unexpired token.
func (s *Sessions) Lookup(ctx context.Context, tok string) (string, error) {
	if tok == "" {
		return "", ErrNoSession
	}
	var admin, exp string
	err := s.DB.QueryRowContext(ctx, `SELECT admin_user_id, expires_at FROM sessions WHERE token_hash=?`, hashToken(tok)).Scan(&admin, &exp)
	if err != nil {
		return "", ErrNoSession
	}
	t, err := time.Parse(time.RFC3339, exp)
	if err != nil || !s.now().Before(t) {
		return "", ErrNoSession
	}
	return admin, nil
}

func (s *Sessions) Delete(ctx context.Context, tok string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, hashToken(tok))
	return err
}

// Purge removes expired sessions.
func (s *Sessions) Purge(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, s.now().Format(time.RFC3339))
	return err
}

// ---- rate limiting ----

// Limiter blocks a key after Max failures within Window.
type Limiter struct {
	Max    int
	Window time.Duration
	Now    func() time.Time

	mu    sync.Mutex
	fails map[string][]time.Time
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Limiter) prune(key string) []time.Time {
	cut := l.now().Add(-l.Window)
	var keep []time.Time
	for _, t := range l.fails[key] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(l.fails, key)
	} else {
		l.fails[key] = keep
	}
	return keep
}

// Allow reports whether an attempt may proceed, and how long to wait if not.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := l.prune(key)
	if len(keep) < l.Max {
		return true, 0
	}
	return false, keep[0].Add(l.Window).Sub(l.now())
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string][]time.Time{}
	}
	l.fails[key] = append(l.prune(key), l.now())
}

func (l *Limiter) Succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
