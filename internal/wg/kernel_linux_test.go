//go:build linux

package wg

import (
	"os/exec"
	"testing"
)

func kernelOrSkip(t *testing.T) Backend {
	t.Helper()
	if out, err := exec.Command("ip", "link", "add", "wgkprobe", "type", "wireguard").CombinedOutput(); err != nil {
		t.Skipf("kernel WireGuard unavailable here: %s", out)
	}
	exec.Command("ip", "link", "del", "wgkprobe").Run()
	k, err := NewKernel()
	if err != nil {
		t.Skipf("wgctrl unavailable: %v", err)
	}
	t.Cleanup(func() { k.Close() })
	return k
}
