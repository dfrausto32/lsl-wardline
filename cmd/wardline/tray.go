package main

import (
	"strings"

	"lsl-wardline/internal/agent"
)

// The tray icon's menu and look, kept apart from Windows so they can be tested anywhere.

type trayItem struct {
	Label     string
	Action    string // window, web, refresh, disconnect, connect, quit
	Disabled  bool
	Separator bool
}

func trayMenu(state agent.State, headline string) []trayItem {
	running := state != agent.StateOff
	items := []trayItem{
		{Label: headline, Disabled: true},
		{Separator: true},
		{Label: "Open Wardline", Action: "window"},
		{Label: "Open the web app", Action: "web"},
		{Separator: true},
	}
	if running {
		items = append(items, trayItem{Label: "Disconnect", Action: "disconnect"})
	} else {
		items = append(items, trayItem{Label: "Connect", Action: "connect"})
	}
	items = append(items,
		trayItem{Label: "Refresh security now", Action: "refresh", Disabled: !running},
		trayItem{Separator: true},
		trayItem{Label: "Quit this icon", Action: "quit"},
	)
	return items
}

// trayIcon names which picture to show: green when connected, amber for connecting or a problem, grey when off.
func trayIcon(state agent.State) string {
	switch state {
	case agent.StateConnected:
		return "connected"
	case agent.StateOff:
		return "off"
	}
	return "problem"
}

// trayTooltip is what shows when the mouse rests on the icon. Windows allows 127 characters.
func trayTooltip(state agent.State, headline, address string) string {
	tip := "Wardline: " + headline
	if state == agent.StateConnected && address != "" {
		tip += " (" + strings.SplitN(address, "/", 2)[0] + ")"
	}
	if len(tip) > 127 {
		tip = tip[:124] + "..." // plain dots, so the cut can never end in the middle of a character or run over
	}
	return tip
}
