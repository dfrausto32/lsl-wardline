package api

import (
	"fmt"
	"net/http"

	"lsl-wardline/internal/portmap"
)

// handleRouterForwarding asks the router to send the server port to this computer. It is optional and
// only offered when this computer is the server; every outcome comes back as plain words, and a failure
// points to the steps the person can do by hand.
func (s *Server) handleRouterForwarding(w http.ResponseWriter, r *http.Request, _ string) {
	if s.d.OpenRouterPort == nil {
		s.fail(w, http.StatusConflict, "This is only available when this computer is your server.")
		return
	}
	res := s.d.OpenRouterPort(r.Context())
	msg := ""
	switch {
	case res.Opened && res.Shared:
		msg = fmt.Sprintf("Your router now sends port %d to this computer, but your internet provider shares one public address between its customers, so devices outside your network may still not reach it. Ask your provider for a public address.", defaultListenPort)
	case res.Opened:
		msg = fmt.Sprintf("Done. Your router now sends port %d to this computer.", defaultListenPort)
	case res.Reason == portmap.NoRouter:
		msg = "Your router did not answer. Many routers have this feature turned off. Follow the steps below instead."
	default:
		msg = "Your router would not allow it. Follow the steps below instead."
	}
	s.json(w, http.StatusOK, map[string]any{"opened": res.Opened, "shared": res.Shared, "router_address": res.RouterAddress, "message": msg})
}
