//go:build !windows

package main

import (
	"reflect"
	"testing"
)

func TestServiceArgsUseSudoOnlyWhenNotRoot(t *testing.T) {
	if got, want := serviceArgs("stop", 0), []string{"systemctl", "stop", "wardline-agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root: got %v want %v", got, want)
	}
	if got, want := serviceArgs("start", 1000), []string{"sudo", "systemctl", "start", "wardline-agent"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("user: got %v want %v", got, want)
	}
}
