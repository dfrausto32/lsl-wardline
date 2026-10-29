//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// desktopShortcut is on the Public desktop, so it shows for everyone who uses this computer.
func desktopShortcut() string {
	pub := os.Getenv("PUBLIC")
	if pub == "" {
		pub = `C:\Users\Public`
	}
	return filepath.Join(pub, "Desktop", "Wardline.url")
}

func writeDesktopShortcut(webURL string) error {
	return os.WriteFile(desktopShortcut(), []byte(shortcutContent(webURL, filepath.Join(programDir(), "wardline.exe"))), 0o644)
}

func removeDesktopShortcut() { _ = os.Remove(desktopShortcut()) }

// openInBrowser opens an address in the person's default web browser.
func openInBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
