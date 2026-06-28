package main

import (
	"strings"

	"github.com/matt0x6f/irc-client/internal/ircurl"
	"github.com/matt0x6f/irc-client/internal/logger"
)

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
		a.emit("deeplink:add-network", map[string]any{
			"host": link.Host, "port": link.Port, "tls": link.TLS, "channel": firstChannel,
		})
	case 1:
		a.emit("deeplink:join", map[string]any{
			"networkId": matches[0].NetworkID, "targets": targets,
		})
	default:
		cands := make([]map[string]any, len(matches))
		for i, m := range matches {
			cands[i] = map[string]any{"networkId": m.NetworkID, "name": m.Name}
		}
		a.emit("deeplink:disambiguate", map[string]any{
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
