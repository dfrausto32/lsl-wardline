// Package log is the one logger Wardline uses. It redacts anything that looks
// like key material or a credential before it reaches the output.
package log

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

const marker = "[redacted]"

// keyShaped matches a base64-encoded 32-byte value, which is what WireGuard
// private, public and preshared keys look like.
var keyShaped = regexp.MustCompile(`[A-Za-z0-9+/]{43}=`)

var sensitiveNames = []string{"password", "passwd", "secret", "token", "authorization", "cookie", "private", "preshared", "psk", "totp", "ciphertext"}

// Redact removes key-shaped values from free text.
func Redact(s string) string {
	return keyShaped.ReplaceAllString(s, marker)
}

func sensitive(name string) bool {
	n := strings.ToLower(name)
	if strings.Contains(n, "public") {
		return false
	}
	for _, s := range sensitiveNames {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

func isPublicKeyAttr(key string) bool {
	return key == "public_key" || strings.HasSuffix(key, "_public_key")
}

func replace(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
		return a
	}
	if a.Key != slog.MessageKey && sensitive(a.Key) {
		return slog.String(a.Key, marker)
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		if isPublicKeyAttr(a.Key) {
			return a
		}
		return slog.String(a.Key, Redact(v.String()))
	case slog.KindAny:
		return slog.String(a.Key, Redact(fmt.Sprint(v.Any())))
	}
	return a
}

// New returns a JSON logger at the given level (debug, info, warn, error).
func New(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l, ReplaceAttr: replace}))
}
