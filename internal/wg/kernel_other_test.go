//go:build !linux

package wg

import "testing"

func kernelOrSkip(t *testing.T) Backend {
	t.Helper()
	t.Skip("kernel WireGuard backend is Linux only")
	return nil
}
