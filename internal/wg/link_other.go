//go:build !linux

package wg

import (
	"errors"
	"net/netip"
)

// setAddress is not implemented outside Linux yet. Windows and macOS device
// agents arrive in phase 2 and decide their own approach.
func setAddress(string, netip.Prefix) error {
	return errors.New("assigning interface addresses is not supported on this system yet")
}
