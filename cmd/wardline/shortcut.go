package main

import "strings"

// shortcutContent is an Internet Shortcut file: double-clicking it opens the web app in the person's
// own browser, with the Wardline icon from the program. Windows expects Windows line endings.
func shortcutContent(url, iconFile string) string {
	return strings.Join([]string{"[InternetShortcut]", "URL=" + url, "IconFile=" + iconFile, "IconIndex=0", ""}, "\r\n")
}
