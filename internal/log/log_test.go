package log

import (
	"bytes"
	"strings"
	"testing"
)

const fakeKey = "yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk="

func TestRedactsKeyShapedValuesInMessageAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "info")
	l.Info("generated "+fakeKey, "note", "value "+fakeKey, "other", 3)
	if strings.Contains(buf.String(), fakeKey) {
		t.Fatalf("key leaked into log output: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "[redacted]") {
		t.Fatalf("expected redaction marker: %s", buf.String())
	}
}

func TestRedactsSensitiveAttrNames(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "info")
	l.Info("sign in", "password", "hunter2", "totp_secret", "JBSWY3DP", "token", "abc123", "Authorization", "Bearer x", "preshared_key", "zzz", "email", "a@b.c")
	out := buf.String()
	for _, s := range []string{"hunter2", "JBSWY3DP", "abc123", "Bearer x", "zzz"} {
		if strings.Contains(out, s) {
			t.Errorf("%q leaked: %s", s, out)
		}
	}
	if !strings.Contains(out, "a@b.c") {
		t.Errorf("non-sensitive value should remain: %s", out)
	}
}

func TestRedactsInsideGroupsAndWith(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "info").With("private_key", fakeKey).WithGroup("g")
	l.Info("x", "secret", "s3cret")
	out := buf.String()
	if strings.Contains(out, fakeKey) || strings.Contains(out, "s3cret") {
		t.Fatalf("leak: %s", out)
	}
}

func TestPublicKeyAttrIsKept(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "info")
	l.Info("device added", "public_key", fakeKey)
	if !strings.Contains(buf.String(), fakeKey) {
		t.Fatalf("public_key attr should be kept: %s", buf.String())
	}
}

func TestRedactString(t *testing.T) {
	got := Redact("error: bad key " + fakeKey + " end")
	if strings.Contains(got, fakeKey) {
		t.Fatal(got)
	}
}

func TestLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "warn")
	l.Info("quiet")
	if buf.Len() != 0 {
		t.Fatal("info should be filtered at warn level")
	}
}
