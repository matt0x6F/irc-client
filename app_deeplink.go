package main

import (
	"strings"

	"github.com/matt0x6f/irc-client/internal/ircurl"
	"github.com/matt0x6f/irc-client/internal/logger"
)

// NetworkPrefill carries deep-link data for a network Cascade doesn't have saved
// yet, so the settings window can open the Add Network form prefilled.
type NetworkPrefill struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	TLS     bool   `json:"tls"`
	Channel string `json:"channel"`
}

func (a *App) setPendingNetworkPrefill(p *NetworkPrefill) {
	a.mu.Lock()
	a.pendingNetworkPrefill = p
	a.mu.Unlock()
}

// GetPendingNetworkPrefill returns the pending Add Network prefill (if any) and
// clears it, so it is consumed exactly once. Bound for the settings window.
func (a *App) GetPendingNetworkPrefill() *NetworkPrefill {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pendingNetworkPrefill
	a.pendingNetworkPrefill = nil
	return p
}

// PendingDeepLink is a deep-link action emitted before the webview was ready,
// buffered so the frontend can drain it on mount.
type PendingDeepLink struct {
	Event string         `json:"event"`
	Data  map[string]any `json:"data"`
}

// dispatchOrBuffer emits the event for live listeners and, until the frontend
// has signalled readiness, also buffers it so a cold-start link isn't lost when
// it fires before the webview mounts its listeners.
func (a *App) dispatchOrBuffer(event string, data map[string]any) {
	a.emit(event, data)
	a.mu.Lock()
	if !a.frontendReady {
		a.pendingDeepLink = &PendingDeepLink{Event: event, Data: data}
	}
	a.mu.Unlock()
}

// DrainPendingDeepLink marks the frontend ready (so later links emit live only)
// and returns and clears any buffered cold-start deep link. Bound; called once
// by the main window on startup.
func (a *App) DrainPendingDeepLink() *PendingDeepLink {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.frontendReady = true
	p := a.pendingDeepLink
	a.pendingDeepLink = nil
	return p
}

// isDeepLinkArg reports whether a CLI arg looks like one of our schemes.
func isDeepLinkArg(arg string) bool {
	return strings.HasPrefix(arg, "irc://") || strings.HasPrefix(arg, "ircs://")
}

// processStartupArgs scans os.Args for a deep link passed at cold start
// (Windows/Linux protocol launch). macOS delivers cold-start URLs via the
// ApplicationLaunchedWithUrl event instead, so this is a no-op there.
func (a *App) processStartupArgs(args []string) {
	for _, arg := range args {
		if isDeepLinkArg(arg) {
			a.handleDeepLink(arg)
		}
	}
}

// NetworkMatch is a saved network that matches a deep link's host.
type NetworkMatch struct {
	NetworkID int64  `json:"networkId"`
	Name      string `json:"name"`
}

// handleDeepLink parses an irc/ircs URL and emits exactly one frontend event:
// add-network (unknown host), join (one match), or disambiguate (many matches).
// It is the single entry point for all platform arrival paths. Safe to call on
// a background goroutine: it only parses and emits.
func (a *App) handleDeepLink(raw string) {
	link, err := ircurl.Parse(raw)
	if err != nil {
		logger.Log.Warn().Err(err).Msg("deeplink: ignoring unparseable url")
		return
	}

	targets := make([]map[string]any, 0, len(link.Targets))
	firstChannel := ""
	for _, t := range link.Targets {
		targets = append(targets, map[string]any{"name": t.Name, "isNick": t.IsNick, "key": t.Key})
		if firstChannel == "" && !t.IsNick {
			firstChannel = t.Name
		}
	}

	matches := a.resolveMatches(link.Host)
	switch len(matches) {
	case 0:
		a.setPendingNetworkPrefill(&NetworkPrefill{
			Host: link.Host, Port: link.Port, TLS: link.TLS, Channel: firstChannel,
		})
		a.dispatchOrBuffer("deeplink:add-network", map[string]any{
			"host": link.Host, "port": link.Port, "tls": link.TLS, "channel": firstChannel,
		})
	case 1:
		a.dispatchOrBuffer("deeplink:join", map[string]any{
			"networkId": matches[0].NetworkID, "targets": targets,
		})
	default:
		cands := make([]map[string]any, len(matches))
		for i, m := range matches {
			cands[i] = map[string]any{"networkId": m.NetworkID, "name": m.Name}
		}
		a.dispatchOrBuffer("deeplink:disambiguate", map[string]any{
			"candidates": cands, "targets": targets,
		})
	}
}

// resolveMatches maps a host to saved networks, attaching display names.
func (a *App) resolveMatches(host string) []NetworkMatch {
	ids := a.findNetworksByAddress(host)
	if len(ids) == 0 {
		return nil
	}
	names := map[int64]string{}
	if networks, err := a.storage.GetNetworks(); err == nil {
		for _, n := range networks {
			names[n.ID] = n.Name
		}
	}
	out := make([]NetworkMatch, 0, len(ids))
	for _, id := range ids {
		out = append(out, NetworkMatch{NetworkID: id, Name: names[id]})
	}
	return out
}
