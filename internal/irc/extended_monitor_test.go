package irc

import (
	"slices"
	"testing"
)

// TestRequestedCapsIncludesExtendedMonitor: extended-monitor is ratified and must
// be in the wanted set so that, when the server offers it, MONITORed nicks deliver
// AWAY/ACCOUNT/CHGHOST/SETNAME even when we share no channel with them.
func TestRequestedCapsIncludesExtendedMonitor(t *testing.T) {
	found := false
	for _, c := range requestedCaps {
		if c == "extended-monitor" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("requestedCaps must include extended-monitor; got %v", requestedCaps)
	}
}

// TestExtendedMonitorRequestedWhenOffered: the shared offered∩wanted filter must
// pick up extended-monitor when the server advertises it.
func TestExtendedMonitorRequestedWhenOffered(t *testing.T) {
	got := capsToRequest("server-time extended-monitor away-notify", map[string]bool{}, false)
	if !slices.Contains(got, "extended-monitor") {
		t.Errorf("expected extended-monitor to be requested; got %v", got)
	}
}

// TestExtendedMonitorRosterUpdateForMonitoredOnlyNick: with extended-monitor the
// server pushes AWAY for a nick we only monitor (never shared a channel with). The
// roster path stores it unconditionally, so a later UserMetaFor lookup must reflect
// the away state — this is what the Buddies pane / DM dots read.
func TestExtendedMonitorRosterUpdateForMonitoredOnlyNick(t *testing.T) {
	c, counter := newUserMetaTestClient(t)

	c.handleAway(parse(t, ":buddybot!b@h AWAY :afk"))
	waitForMetaCount(t, counter, 1)

	meta, ok := c.UserMetaFor("buddybot")
	if !ok || !meta.Away || meta.AwayMessage != "afk" {
		t.Fatalf("monitored-only nick away state: ok=%v meta=%+v", ok, meta)
	}
}
