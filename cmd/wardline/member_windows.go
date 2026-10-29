//go:build windows

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// What a computer that has joined the network gets besides the background service: a place the tray
// icon and window can read its status from, the tray icon starting at sign-in, and two shortcuts.

const runKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "Wardline"

// sidUsers is the built-in group of every ordinary user of the computer.
const sidUsers = "*S-1-5-32-545"

func installedExe() string { return filepath.Join(programDir(), "wardline.exe") }

func shortcutPaths() []string {
	pub := os.Getenv("PUBLIC")
	if pub == "" {
		pub = `C:\Users\Public`
	}
	data := programData()
	return []string{
		filepath.Join(pub, "Desktop", "Wardline.lnk"),
		filepath.Join(data, "Microsoft", "Windows", "Start Menu", "Programs", "Wardline.lnk"),
	}
}

func makeShortcut(path, target, args, icon, description string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "-")
	cmd.Stdin = bytes.NewBufferString(shortcutScript(path, target, args, icon, description))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// setUpMemberExtras is best effort: a missing shortcut must not undo a working connection.
func setUpMemberExtras(out io.Writer, statusFile string) {
	exe := installedExe()
	// Everyone may read the status, which holds nothing secret; only the system may write it.
	dir := filepath.Dir(statusFile)
	if err := os.MkdirAll(dir, 0o755); err == nil {
		exec.Command("icacls", dir, "/grant", sidUsers+":(OI)(CI)R").Run()
	}
	if k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, runKeyPath, registry.SET_VALUE); err == nil {
		k.SetStringValue(runValueName, `"`+exe+`" tray`)
		k.Close()
	}
	made := 0
	for _, p := range shortcutPaths() {
		if err := makeShortcut(p, exe, "window", exe, "Wardline: your private network"); err == nil {
			made++
		}
	}
	if made > 0 {
		fmt.Fprintln(out, "A Wardline icon was added to your desktop and Start menu. The Wardline tray icon appears the next time you sign in, or when you open Wardline.")
	}
}

func removeMemberExtras(statusFile string) {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, runKeyPath, registry.SET_VALUE); err == nil {
		k.DeleteValue(runValueName)
		k.Close()
	}
	for _, p := range shortcutPaths() {
		os.Remove(p)
	}
	os.RemoveAll(filepath.Dir(statusFile))
}

// runElevated runs this program again with the given command behind Windows' permission prompt.
func runElevated(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return windows.ShellExecute(0, verb, file, params, dir, windows.SW_HIDE)
}

// serviceExists asks Windows whether a service is installed. It needs no administrator rights.
func serviceExists(name string) bool {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(scm)
	n, _ := windows.UTF16PtrFromString(name)
	h, err := windows.OpenService(scm, n, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	windows.CloseServiceHandle(h)
	return true
}
