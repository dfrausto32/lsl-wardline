//go:build !linux && !windows

package main

import "lsl-wardline/internal/agent"

// There is no way yet to set a Mac's names from here; it keeps working and says names are unavailable.
func newNameClient() agent.NameClient { return nil }
