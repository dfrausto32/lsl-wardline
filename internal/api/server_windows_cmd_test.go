package api

import (
	"strings"
	"testing"
)

func TestInstallInstructionsIncludeAWindowsCommand(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var created struct {
		Install map[string]string `json:"install"`
	}
	h.mustCall("POST", "/api/servers", nil, &created, 201)
	cmd := created.Install["windows"]
	code := tokenRe.FindStringSubmatch(created.Install["docker"])[1]
	for _, want := range []string{`.\wardline.exe gateway install`, "--control-url", "--token " + code} {
		if !strings.Contains(cmd, want) {
			t.Errorf("windows command %q is missing %q", cmd, want)
		}
	}
	if strings.Contains(cmd, "\n") {
		t.Errorf("the windows command should be one line: %q", cmd)
	}
}

// When this computer is also the server, the install code goes straight to it and the page says so.
func TestAServerOnTheSameComputerIsRegisteredWithoutCopyingACommand(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var got string
	h.api.d.OnServerCreated = func(code string) { got = code }
	var created struct {
		Local   bool              `json:"local"`
		Install map[string]string `json:"install"`
	}
	h.mustCall("POST", "/api/servers", nil, &created, 201)
	if !created.Local {
		t.Fatal("the page should be told this computer is the server")
	}
	if want := tokenRe.FindStringSubmatch(created.Install["docker"])[1]; got == "" || got != want {
		t.Fatalf("the hook got %q, want the install code %q", got, want)
	}
}

func TestWithoutALocalServerTheCommandsAreStillShown(t *testing.T) {
	h := newHarness(t)
	h.setup()
	var created struct {
		Local bool `json:"local"`
	}
	h.mustCall("POST", "/api/servers", nil, &created, 201)
	if created.Local {
		t.Fatal("no local server is configured")
	}
}
