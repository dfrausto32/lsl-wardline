package main

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
)

// The commands that set a computer to use the network's names. The values come from the control plane, so
// they are checked here and never pasted into a command as they arrive.

var (
	ifaceRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,14}$`)
	domainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

func checkNameSettings(domain, server string) (netip.Addr, error) {
	if !domainRE.MatchString(domain) {
		return netip.Addr{}, errors.New("the control plane sent a name ending this computer does not accept")
	}
	a, err := netip.ParseAddr(server)
	if err != nil || !a.Is4() {
		return netip.Addr{}, errors.New("the control plane sent a server address this computer does not accept")
	}
	return a, nil
}

// resolvectlApply is the systemd-resolved commands that send the network's names to the server, through the tunnel interface.
func resolvectlApply(iface, domain, server string) ([][]string, error) {
	if !ifaceRE.MatchString(iface) {
		return nil, errors.New("unexpected tunnel name")
	}
	a, err := checkNameSettings(domain, server)
	if err != nil {
		return nil, err
	}
	return [][]string{
		{"resolvectl", "dns", iface, a.String()},
		// No leading "~": the ending is both where these names go and a search ending, so "ping office-pc" works.
		{"resolvectl", "domain", iface, domain},
	}, nil
}

func resolvectlClear(iface string) []string { return []string{"resolvectl", "revert", iface} }

const nrptRemoveOurs = `Get-DnsClientNrptRule | Where-Object { $_.Comment -eq 'Wardline' } | Remove-DnsClientNrptRule -Force`

// nrptApplyScript adds the one Windows name rule: names ending in domain go to the server, nothing else does.
func nrptApplyScript(domain, server string) (string, error) {
	a, err := checkNameSettings(domain, server)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$ErrorActionPreference = 'Stop'\n%s\nAdd-DnsClientNrptRule -Namespace '.%s' -NameServers '%s' -Comment 'Wardline'\nClear-DnsClientCache\n",
		nrptRemoveOurs, domain, a.String()), nil
}

func nrptClearScript() string {
	return "$ErrorActionPreference = 'Stop'\n" + nrptRemoveOurs + "\nClear-DnsClientCache\n"
}
