//go:build windows

package main

import (
	"embed"
	"os"
	"os/exec"
	"time"

	"fyne.io/systray"
	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"

	"lsl-wardline/internal/agent"
)

//go:embed tray-connected.ico tray-problem.ico tray-off.ico
var trayIcons embed.FS

// trayActions are every item the menu can ever show; trayMenu decides which are visible.
var trayActions = []string{"window", "web", "disconnect", "connect", "refresh", "quit"}

func runTray() error {
	// One tray icon per person: a second copy quietly does nothing.
	name, _ := windows.UTF16PtrFromString(`Local\WardlineTray`)
	if _, err := windows.CreateMutex(nil, false, name); err == windows.ERROR_ALREADY_EXISTS {
		return nil
	}
	statusPath := defaultStatusFile()
	read := func() agent.Status { s, _ := agent.LoadStatus(statusPath); return s }

	systray.Run(func() {
		systray.SetTitle("Wardline")
		head := systray.AddMenuItem("Wardline", "")
		head.Disable()
		systray.AddSeparator()
		items := map[string]*systray.MenuItem{
			"window": systray.AddMenuItem("Open Wardline", ""),
			"web":    systray.AddMenuItem("Open the web app", ""),
		}
		systray.AddSeparator()
		items["disconnect"] = systray.AddMenuItem("Disconnect", "")
		items["connect"] = systray.AddMenuItem("Connect", "")
		items["refresh"] = systray.AddMenuItem("Refresh security now", "")
		systray.AddSeparator()
		items["quit"] = systray.AddMenuItem("Quit this icon", "")

		do := func(action string) {
			switch action {
			case "window":
				if exe, err := os.Executable(); err == nil {
					exec.Command(exe, "window").Start()
				}
			case "web":
				if u := read().ControlURL; u != "" {
					openInBrowser(u)
				}
			case "disconnect":
				runElevated("agent", "stop")
			case "connect":
				runElevated("agent", "start")
			case "refresh":
				runElevated("agent", "refresh")
			case "quit":
				systray.Quit()
			}
		}
		for action, it := range items {
			action, it := action, it
			go func() {
				for range it.ClickedCh {
					do(action)
				}
			}()
		}
		systray.SetOnTapped(func() { do("window") })

		update := func() {
			s := read()
			state, headline := s.Describe(time.Now())
			head.SetTitle(headline)
			ico, _ := trayIcons.ReadFile("tray-" + trayIcon(state) + ".ico")
			systray.SetIcon(ico)
			systray.SetTooltip(trayTooltip(state, headline, s.Address))
			shown := map[string]trayItem{}
			for _, it := range trayMenu(state, headline) {
				if it.Action != "" {
					shown[it.Action] = it
				}
			}
			for _, a := range trayActions {
				it, ok := shown[a]
				switch {
				case !ok:
					items[a].Hide()
				case it.Disabled:
					items[a].Show()
					items[a].Disable()
				default:
					items[a].Show()
					items[a].Enable()
				}
			}
		}
		update()
		go func() {
			for range time.Tick(4 * time.Second) {
				update()
			}
		}()
	}, func() {})
	return nil
}

// ensureTray starts the tray icon for the signed-in person if it is not already running.
func ensureTray() {
	if exe, err := os.Executable(); err == nil {
		exec.Command(exe, "tray").Start()
	}
}

func platformRootCommands(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use: "tray", Short: "Show the Wardline tray icon", Hidden: true,
		RunE: func(*cobra.Command, []string) error { return runTray() },
	}, &cobra.Command{
		Use: "window", Short: "Show how this computer is doing, in a window", Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error { return runStatusWindow(cmd.Context()) },
	})
}
