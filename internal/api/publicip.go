package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// publicAddressService answers with the caller's own address as plain text. It sees the address of
// whoever asks, which is the whole point, and is only contacted when an admin presses the button.
const publicAddressService = "https://api.ipify.org"

func (s *Server) handlePublicAddress(w http.ResponseWriter, r *http.Request, _ string) {
	find := s.d.PublicIP
	if find == nil {
		find = lookupPublicIP
	}
	addr, err := find(r.Context())
	if err != nil {
		s.d.Log.Warn("could not look up the public address", "error", err.Error())
		s.fail(w, http.StatusBadGateway, "Could not find your public address. Check that this computer is online, or type it in yourself.")
		return
	}
	s.json(w, http.StatusOK, map[string]string{"address": addr})
}

func lookupPublicIP(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicAddressService, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the address service answered %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 64))
	if err != nil {
		return "", err
	}
	return checkPublicAddress(string(b))
}

// checkPublicAddress accepts only an address that really is on the public internet, so a wrong answer
// (or a network that rewrites it) is never offered as the address to give to devices.
func checkPublicAddress(s string) (string, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return "", errors.New("the answer was not an address")
	}
	cgnat := netip.MustParsePrefix("100.64.0.0/10") // shared addresses used inside some providers' networks
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip) {
		return "", errors.New("the address is not a public one")
	}
	return ip.String(), nil
}
