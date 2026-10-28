package wg

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// These tests describe the container the project is developed in. They do not
// test Wardline code; they tell us which WireGuard backends can run here.

func TestEnvToolsPresent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dev container checks only apply on Linux")
	}
	for _, tool := range []string{"ip", "wg"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Errorf("%s is missing from the dev container: %v", tool, err)
		}
	}
}

func TestEnvTunDevice(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dev container checks only apply on Linux")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Errorf("/dev/net/tun is missing; the userspace backend cannot run: %v", err)
	}
}

// TestEnvKernelWireGuard records whether the kernel backend can run here. It
// never fails: the userspace backend carries everything when the kernel module
// is absent. The result is logged so smoke.ps1 can surface it.
func TestEnvKernelWireGuard(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dev container checks only apply on Linux")
	}
	out, err := exec.Command("ip", "link", "add", "wgenvtest", "type", "wireguard").CombinedOutput()
	if err != nil {
		t.Logf("KERNEL-WIREGUARD: unavailable (%s)", strings.TrimSpace(string(out)))
		return
	}
	defer exec.Command("ip", "link", "del", "wgenvtest").Run()
	t.Log("KERNEL-WIREGUARD: available")
}
