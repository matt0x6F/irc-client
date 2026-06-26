package irc

import (
	"strings"

	"github.com/ergochat/irc-go/ircmsg"
)

// applyMyInfo records the server software version from RPL_MYINFO (004). Params are
// <client> <servername> <version> <usermodes> <chanmodes> [chanmodes-with-params];
// the version token later drives server-family detection (see SoftwareFamily).
func (c *IRCClient) applyMyInfo(e ircmsg.Message) {
	if len(e.Params) < 3 {
		return
	}
	c.mu.Lock()
	c.serverCapabilities.Software = e.Params[2]
	c.mu.Unlock()
}

// detectServerFamily identifies the IRC server software family from server-sent
// signals only — never from any user-configured value such as a connection label.
//
// Primary signal is the version token from RPL_MYINFO (004) (e.g. "solanum-1.0-dev",
// "UnrealIRCd-6.1.0", "InspIRCd-4", "ergo-2.13.0"). When that is blank or unrecognized
// (some networks customize or hide it), it falls back to an ISUPPORT-shape fingerprint:
// the EXTBAN prefix is characteristic per family. The fingerprint only returns a family
// when the signal is unambiguous — a "$" prefix uniquely marks the charybdis/solanum
// family, whereas "~" is shared by UnrealIRCd and InspIRCd and so is left unresolved.
//
// The returned id is stable and consumed by the frontend mode-label resolver. An empty
// string means "unknown", which the resolver handles with a safe generic fallback.
func detectServerFamily(version string, extbanPrefix rune) string {
	v := strings.ToLower(version)
	switch {
	case strings.Contains(v, "solanum"),
		strings.Contains(v, "charybdis"),
		strings.Contains(v, "ircd-seven"),
		strings.Contains(v, "ratbox"):
		return "solanum"
	case strings.Contains(v, "unreal"):
		return "unrealircd"
	case strings.Contains(v, "inspircd"):
		return "inspircd"
	case strings.Contains(v, "ergo"),
		strings.Contains(v, "oragono"):
		return "ergo"
	}

	// Version unrecognized: fall back to an unambiguous ISUPPORT fingerprint.
	if extbanPrefix == '$' {
		return "solanum"
	}
	return ""
}

// SoftwareFamily returns the detected server software family (e.g. "solanum",
// "unrealircd", "inspircd", "ergo"), or "" when unknown. It derives the answer purely
// from server-sent signals captured during registration; see detectServerFamily.
func (c *IRCClient) SoftwareFamily() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.serverCapabilities == nil {
		return ""
	}
	return detectServerFamily(c.serverCapabilities.Software, c.serverCapabilities.ExtbanPrefix)
}
