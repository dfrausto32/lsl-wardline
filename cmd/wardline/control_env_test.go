package main

import "testing"

// A cloud server runs the web app and the server role as one service, switched on from its settings file.
func TestTheServerOnThisComputerCanBeSwitchedOnFromTheEnvironment(t *testing.T) {
	c := controlCmd()
	if v, _ := c.PersistentFlags().GetBool("with-server"); v {
		t.Fatal("it must be off unless someone asks for it")
	}
	t.Setenv("WARDLINE_WITH_SERVER", "1")
	c = controlCmd()
	if v, _ := c.PersistentFlags().GetBool("with-server"); !v {
		t.Fatal("WARDLINE_WITH_SERVER=1 should switch it on")
	}
	if dir, _ := c.PersistentFlags().GetString("server-dir"); dir == "" {
		t.Fatal("the server needs a folder for its identity")
	}
}
