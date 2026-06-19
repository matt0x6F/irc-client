package irc

import "testing"

func TestMonitorOnlineOfflineUpdatesPresence(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// 730 RPL_MONONLINE: comma-separated targets, possibly with !user@host.
	c.handleMonitorPresence(parse(t, ":srv 730 me :alice!a@host,bob!b@host"), true)
	if p := c.MonitorPresence(); !p["alice"] || !p["bob"] {
		t.Fatalf("after 730: %+v", p)
	}

	// 731 RPL_MONOFFLINE: bare nicks.
	c.handleMonitorPresence(parse(t, ":srv 731 me :bob"), false)
	if p := c.MonitorPresence(); !p["alice"] || p["bob"] {
		t.Fatalf("after 731: %+v", p)
	}
}

func TestMonitorPresenceEmitsOnlyOnChange(t *testing.T) {
	c, counter := newUserMetaTestClient(t)
	// metaCounter subscribes to EventUserMetaChanged, not monitor; use a dedicated
	// subscriber for EventMonitorChanged.
	mon := newMetaCounter()
	c.eventBus.Subscribe(EventMonitorChanged, mon)
	_ = counter

	c.setMonitorPresence("carol", true)
	c.setMonitorPresence("carol", true) // no change → no second event
	waitForMetaCount(t, mon, 1)
	if got := mon.snapshotCount(); got != 1 {
		t.Fatalf("EventMonitorChanged emitted %d times, want 1", got)
	}

	c.setMonitorPresence("carol", false) // change → event
	waitForMetaCount(t, mon, 2)
}

func TestMonitorPresenceReturnsCopy(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.setMonitorPresence("dave", true)

	snap := c.MonitorPresence()
	snap["dave"] = false // mutating the snapshot must not affect client state
	if !c.MonitorPresence()["dave"] {
		t.Fatal("MonitorPresence must return a copy, not the live map")
	}
}

func TestMonitorPresenceIgnoresShortReply(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.handleMonitorPresence(parse(t, ":srv 730 me"), true) // no target list
	if len(c.MonitorPresence()) != 0 {
		t.Fatal("a 730 with no target list must not record presence")
	}
}
