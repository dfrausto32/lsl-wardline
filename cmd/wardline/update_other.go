//go:build !linux

package main

import (
	"errors"

	"lsl-wardline/internal/update"
)

// Self-updating is for Linux servers. On Windows, run the new wardline.exe with "agent install" (or the installer).
func systemHost() (update.Host, update.Paths, error) {
	return nil, update.Paths{}, errors.New("automatic updates are only available on Linux servers; on Windows, download the new wardline.exe and run it")
}
