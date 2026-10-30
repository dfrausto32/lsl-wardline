//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"time"

	"lsl-wardline/internal/agent"
)

func readState() (agent.State, string) {
	s, _ := agent.LoadStatus(defaultStatusFile())
	return s.Describe(time.Now())
}

// setConnection stops or starts the service. Without administrator rights it asks Windows for permission
// and waits to see the result, so a plain terminal is enough.
func setConnection(out io.Writer, on bool) error {
	err := setAgentRunning(on)
	if err == nil {
		return nil
	}
	if !serviceExists(agentServiceName) {
		return err
	}
	fmt.Fprintln(out, "Windows will ask for permission.")
	verb := "stop"
	if on {
		verb = "start"
	}
	if err := runElevated("agent", verb); err != nil {
		return errors.New("permission was not given")
	}
	for i := 0; i < 40; i++ {
		time.Sleep(500 * time.Millisecond)
		if st, _ := readState(); (st == agent.StateOff) != on {
			return nil
		}
	}
	return errors.New("it did not change in time; check with: wardline status")
}

func connectionStatus() (string, error) {
	if !serviceExists(agentServiceName) {
		return "", errors.New("Wardline is not set up on this computer")
	}
	st, headline := readState()
	if st == agent.StateOff {
		return "Off. This computer is not on the private network. Turn it on with: wardline on", nil
	}
	return headline, nil
}
