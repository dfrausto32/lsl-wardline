package main

import "strings"

// shortcutContent is an Internet Shortcut file: double-clicking it opens the web app in the person's
// own browser, with the Wardline icon from the program. Windows expects Windows line endings.
func shortcutContent(url, iconFile string) string {
	return strings.Join([]string{"[InternetShortcut]", "URL=" + url, "IconFile=" + iconFile, "IconIndex=0", ""}, "\r\n")
}

// shortcutScript is a PowerShell script that makes a Windows shortcut file (.lnk). Windows has no
// plain-text form of those, so the program asks PowerShell's built-in shortcut helper to make it.
// Values go inside single quotes, so a quote in them is doubled.
func shortcutScript(path, target, args, icon, description string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	return strings.Join([]string{
		"$ws = New-Object -ComObject WScript.Shell",
		"$s = $ws.CreateShortcut(" + q(path) + ")",
		"$s.TargetPath = " + q(target),
		"$s.Arguments = " + q(args),
		"$s.IconLocation = " + q(icon+",0"),
		"$s.Description = " + q(description),
		"$s.Save()",
	}, "\n")
}
