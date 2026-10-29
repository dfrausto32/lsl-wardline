package main

import (
	"strings"
	"testing"
)

func TestTheDesktopShortcutOpensTheWebAppWithTheWardlineIcon(t *testing.T) {
	got := shortcutContent("http://localhost:8080", `C:\Program Files\Wardline\wardline.exe`)
	for _, want := range []string{"[InternetShortcut]", "URL=http://localhost:8080", `IconFile=C:\Program Files\Wardline\wardline.exe`, "IconIndex=0"} {
		if !strings.Contains(got, want) {
			t.Errorf("shortcut is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("a shortcut file uses Windows line endings: %q", got)
	}
}
