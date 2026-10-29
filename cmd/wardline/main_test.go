package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// On Windows, cobra refuses to run a program that Explorer started with a double-click: it prints
// "this is a command line tool" and exits. Wardline is meant to be double-clicked, so that guard
// must be off, or the program does nothing visible at all.
func TestDoubleClickingIsNotBlockedByTheCommandLineLibrary(t *testing.T) {
	newRoot()
	if cobra.MousetrapHelpText != "" {
		t.Fatalf("cobra would block a double-click with %q", cobra.MousetrapHelpText)
	}
}
