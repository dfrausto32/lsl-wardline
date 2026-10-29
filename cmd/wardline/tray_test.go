package main

import (
	"strings"
	"testing"

	"lsl-wardline/internal/agent"
)

func labels(items []trayItem) []string {
	var out []string
	for _, i := range items {
		if i.Separator {
			out = append(out, "-")
		} else {
			out = append(out, i.Label)
		}
	}
	return out
}

func find(items []trayItem, action string) *trayItem {
	for i := range items {
		if items[i].Action == action {
			return &items[i]
		}
	}
	return nil
}

func TestTheTrayMenuWhenConnected(t *testing.T) {
	m := trayMenu(agent.StateConnected, "Connected")
	if m[0].Label != "Connected" || !m[0].Disabled {
		t.Fatalf("the first line is the status, not clickable: %+v", m[0])
	}
	for _, a := range []string{"window", "web", "refresh", "disconnect", "quit"} {
		if find(m, a) == nil {
			t.Errorf("missing %q in %v", a, labels(m))
		}
	}
	if find(m, "connect") != nil {
		t.Error("a connected computer is offered Disconnect, not Connect")
	}
	if d := find(m, "refresh"); d.Disabled {
		t.Error("refresh should work while connected")
	}
}

func TestTheTrayMenuWhenNotRunning(t *testing.T) {
	m := trayMenu(agent.StateOff, "Wardline is not running on this computer.")
	if find(m, "connect") == nil || find(m, "disconnect") != nil {
		t.Fatalf("offer Connect when stopped: %v", labels(m))
	}
	if find(m, "refresh").Disabled != true {
		t.Error("nothing to refresh while stopped")
	}
}

func TestTheTrayIconAndTooltipFollowTheState(t *testing.T) {
	for state, want := range map[agent.State]string{agent.StateConnected: "connected", agent.StateConnecting: "problem", agent.StateProblem: "problem", agent.StateOff: "off"} {
		if got := trayIcon(state); got != want {
			t.Errorf("%s: %q want %q", state, got, want)
		}
	}
	tip := trayTooltip(agent.StateConnected, "Connected", "10.1.2.3/24")
	if !strings.Contains(tip, "Wardline") || !strings.Contains(tip, "Connected") || !strings.Contains(tip, "10.1.2.3") || strings.Contains(tip, "/24") {
		t.Fatalf("tooltip %q", tip)
	}
	if len(trayTooltip(agent.StateProblem, strings.Repeat("x", 300), "")) > 127 {
		t.Fatal("Windows tooltips are limited to 127 characters")
	}
}

func TestShortcutScriptQuotesEverything(t *testing.T) {
	s := shortcutScript(`C:\Users\Public\Desktop\Wardline.lnk`, `C:\Program Files\Wardline\wardline.exe`, "window", `C:\Program Files\Wardline\wardline.exe`, "Dylan's network")
	for _, want := range []string{"WScript.Shell", "CreateShortcut('C:\\Users\\Public\\Desktop\\Wardline.lnk')", "TargetPath = 'C:\\Program Files\\Wardline\\wardline.exe'", "Arguments = 'window'", "Dylan''s network", "$s.Save()"} {
		if !strings.Contains(s, want) {
			t.Errorf("script is missing %q:\n%s", want, s)
		}
	}
}
