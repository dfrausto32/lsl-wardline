//go:build windows

package main

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// prepareConsole makes the program quiet when double-clicked and still talkative from a terminal.
// The program is built as a Windows app, so it opens no black window by itself. When it was started
// from a terminal, attach to that terminal so commands print their output there.
func prepareConsole() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return // output already goes somewhere (a pipe, a file, or a console)
	}
	attach := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	if ok, _, _ := attach.Call(^uintptr(0)); ok == 0 { // the parent has no console: started from Explorer
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = out, out
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = in
	}
}
