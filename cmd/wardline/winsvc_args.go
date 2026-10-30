package main

import (
	"fmt"
	"net"
	"strconv"
)

// The pieces of the Windows service installs that do not touch Windows, so they can be tested anywhere.

const (
	agentServiceName   = "WardlineAgent"
	controlServiceName = "WardlineControl"
	gatewayServiceName = "WardlineGateway"
)

func controlServiceArgs(listen, data, publicURL string) []string {
	return []string{"control", "--listen", listen, "--data", data, "--public-url", publicURL}
}

// gatewayServiceArgs has no --token on purpose: the one-time code is used while installing, and a
// service's command line can be read by every signed-in user.
func gatewayServiceArgs(controlURL, stateDir, iface, name string) []string {
	return []string{"gateway", "--control-url", controlURL, "--state-dir", stateDir, "--interface", iface, "--name", name}
}

type fwRule struct{ Name, Proto, Port, Profile string }

func firewallAddArgs(r fwRule) []string {
	return []string{"advfirewall", "firewall", "add", "rule", "name=" + r.Name, "dir=in", "action=allow", "protocol=" + r.Proto, "localport=" + r.Port, "profile=" + r.Profile}
}

func firewallDeleteArgs(name string) []string {
	return []string{"advfirewall", "firewall", "delete", "rule", "name=" + name}
}

const (
	webRuleName    = "Wardline web"
	serverRuleName = "Wardline server"
	namesRuleName  = "Wardline names"
)

// The web app can change who is on the network, so it is only opened on home and work networks, never public ones.
func controlFirewall(listen string) []fwRule {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		port = "8080"
	}
	return []fwRule{{Name: webRuleName, Proto: "TCP", Port: port, Profile: "private,domain"}}
}

// The name service listens only on the server's own network address, so opening its port does not expose it to other networks.
func namesFirewall() []fwRule {
	return []fwRule{{Name: namesRuleName, Proto: "UDP", Port: "53", Profile: "any"}}
}

// The server's port only answers devices that hold a valid key, so it is open on any network.
func gatewayFirewall(port int) []fwRule {
	return []fwRule{{Name: serverRuleName, Proto: "UDP", Port: strconv.Itoa(port), Profile: "any"}}
}

// publicURLFor builds the address other computers use to reach the web app, from this computer's
// address and the listen address (":8080").
func publicURLFor(ip, listen string) (string, error) {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "", fmt.Errorf("the listen address %q needs a port, for example :8080", listen)
	}
	return "http://" + net.JoinHostPort(ip, port), nil
}

// outboundIP is the address of the network connection this computer would use to reach the
// internet, which is the one other computers on its network reach it by. Nothing is sent.
func outboundIP() (string, error) {
	c, err := net.Dial("udp", "192.0.2.1:9")
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// The server's WireGuard port; the web app tells devices to use it.
const gatewayPort = 51820
