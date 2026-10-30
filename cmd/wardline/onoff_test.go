package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOnOffCommandsExistAtTheTop(t *testing.T) {
	root := newRoot()
	for _, name := range []string{"on", "off", "status"} {
		c, _, err := root.Find([]string{name})
		if err != nil || c.Name() != name {
			t.Fatalf("wardline %s is missing: %v", name, err)
		}
	}
}

func TestStatusSaysWhatToDoWhenNotSetUp(t *testing.T) {
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status"})
	err := root.Execute()
	// In a test there is no installed service, so it must answer in words, not crash.
	if err == nil && out.Len() == 0 {
		t.Fatal("status printed nothing")
	}
	if err != nil && strings.TrimSpace(err.Error()) == "" {
		t.Fatal("status failed with no message")
	}
}
