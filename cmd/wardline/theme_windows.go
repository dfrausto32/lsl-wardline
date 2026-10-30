//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// Windows 11 lets a program colour its own title bar and border. Older Windows ignores the request,
// so the window simply keeps its normal frame.
const (
	dwmDarkMode     = 20
	dwmBorderColor  = 34
	dwmCaptionColor = 35
	dwmTextColor    = 36
)

func appsUseDarkTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// themeWindow gives a window the title bar and border colours of the Wardline pages inside it.
func themeWindow(hwnd unsafe.Pointer) {
	if hwnd == nil {
		return
	}
	dark := appsUseDarkTheme()
	c := themeColors(dark)
	set := syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	apply := func(attr uintptr, v uint32) {
		set.Call(uintptr(hwnd), attr, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	}
	var d uint32
	if dark {
		d = 1
	}
	apply(dwmDarkMode, d)
	apply(dwmCaptionColor, c.caption)
	apply(dwmTextColor, c.text)
	apply(dwmBorderColor, c.border)
}
