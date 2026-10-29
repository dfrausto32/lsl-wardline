//go:build !windows

package wintun

// Prepare and PrepareIn do nothing off Windows; only Windows needs the driver library.
func Prepare() error { return nil }

func PrepareIn(string) error { return nil }
