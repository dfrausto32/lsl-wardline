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
