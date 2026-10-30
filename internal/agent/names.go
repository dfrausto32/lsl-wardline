package agent

import (
	"context"
)

// NameClient makes a computer ask the Wardline server for the network's names. It is the computer's own
// name settings and differs by system (Windows, Linux). Both calls can be repeated safely.
type NameClient interface {
	// Apply sets the computer to ask server for names ending in domain, through the tunnel interface.
	Apply(ctx context.Context, iface, domain, server string) error
	// Clear undoes Apply.
	Clear(ctx context.Context, iface string) error
}

// applyNames follows the instructions about names. Not being able to use names is reported, never fatal:
// the tunnel and everything else keep working, and the computer can still be reached by its address.
func (a *Agent) applyNames(ctx context.Context, st DesiredState) {
	status := ""
	switch {
	case st.NamesDomain == "" || st.NamesServer == "":
		if a.cfg.Names != nil {
			if err := a.cfg.Names.Clear(ctx, a.cfg.Iface); err != nil {
				a.cfg.Log.Warn("could not take the network's names off this computer", "error", err.Error())
			}
		}
	case a.cfg.Names == nil:
		status = NamesUnavailable
	default:
		if err := a.cfg.Names.Apply(ctx, a.cfg.Iface, st.NamesDomain, st.NamesServer); err != nil {
			a.cfg.Log.Warn("this computer could not be set up to use the network's names, so use addresses instead", "error", err.Error())
			status = NamesUnavailable
		} else {
			status = NamesOK
		}
	}
	a.mu.Lock()
	a.names = status
	a.mu.Unlock()
}

// clearNames takes the names setting off this computer (the agent is stopping or the device was removed).
func (a *Agent) clearNames(ctx context.Context) {
	if a.cfg.Names == nil {
		return
	}
	if err := a.cfg.Names.Clear(ctx, a.cfg.Iface); err != nil {
		a.cfg.Log.Warn("could not take the network's names off this computer", "error", err.Error())
	}
	a.mu.Lock()
	a.names = ""
	a.mu.Unlock()
}
