package irc

import (
	"strings"
	"testing"
)

func TestIsServiceNick(t *testing.T) {
	services := []string{"NickServ", "ChanServ", "SaslServ", "MemoServ", "HostServ", "OperServ", "BotServ", "Global", "nickserv", "ChanServ"}
	for _, n := range services {
		if !IsServiceNick(n) {
			t.Errorf("IsServiceNick(%q) = false, want true", n)
		}
	}
	people := []string{"alittlefang", "matt0x6f", "alice", "serveradmin", "service_bot"}
	for _, n := range people {
		if IsServiceNick(n) {
			t.Errorf("IsServiceNick(%q) = true, want false", n)
		}
	}
}

func TestDesiredMonitorState(t *testing.T) {
	const self = "matt0x6f"
	cases := []struct {
		name               string
		nick               string
		isBuddy, hasOpenPM bool
		want               bool
	}{
		{"durable buddy only", "alice", true, false, true},
		{"open PM only", "alice", false, true, true},
		{"buddy and open PM", "alice", true, true, true},
		{"neither", "alice", false, false, false},
		{"self with open PM is not monitored", self, false, true, false},
		{"self that is somehow a buddy is still monitored", self, true, false, true},
		{"service nick with open PM is not auto-monitored", "NickServ", false, true, false},
		{"service nick explicitly a buddy is still monitored", "NickServ", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desiredMonitorState(tc.nick, self, tc.isBuddy, tc.hasOpenPM); got != tc.want {
				t.Errorf("desiredMonitorState(%q, self=%q, buddy=%v, openPM=%v) = %v, want %v",
					tc.nick, self, tc.isBuddy, tc.hasOpenPM, got, tc.want)
			}
		})
	}
}

func TestMonitorSetArmsAndDisarms(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.supportsMonitor = true

	c.MonitorSet("alice", true)
	if !c.monitorArmed["alice"] {
		t.Fatal("alice should be armed after MonitorSet(true)")
	}
	c.MonitorSet("alice", true) // idempotent: still armed, no error
	if !c.monitorArmed["alice"] {
		t.Fatal("alice should remain armed")
	}
	c.MonitorSet("alice", false)
	if c.monitorArmed["alice"] {
		t.Fatal("alice should be disarmed after MonitorSet(false)")
	}
}

func TestMonitorSetRespectsLimit(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.supportsMonitor = true
	c.monitorLimit = 1

	c.MonitorSet("a", true)
	c.MonitorSet("b", true) // would exceed the advertised limit → skipped
	if !c.monitorArmed["a"] {
		t.Fatal("a should be armed")
	}
	if c.monitorArmed["b"] {
		t.Fatal("b should be skipped once the MONITOR limit is reached")
	}
}

func TestMonitorSetNoopWithoutSupport(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	// supportsMonitor defaults false
	c.MonitorSet("a", true)
	if len(c.monitorArmed) != 0 {
		t.Fatal("MonitorSet must do nothing when the server lacks MONITOR")
	}
}

func TestMonitorReconcileNickUsesBuddiesAndOpenPMs(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.supportsMonitor = true
	nid := c.networkID
	self := c.network.Nickname

	// alice: a durable buddy (no PM).
	if err := c.storage.AddMonitoredNick(nid, "alice"); err != nil {
		t.Fatal(err)
	}
	// bob: an open PM, not a buddy.
	if _, err := c.storage.GetOrCreatePMConversation(nid, "bob", self); err != nil {
		t.Fatal(err)
	}
	// carol: a PM that has been closed.
	if _, err := c.storage.GetOrCreatePMConversation(nid, "carol", self); err != nil {
		t.Fatal(err)
	}
	if err := c.storage.UpdatePMConversationIsOpen(nid, "carol", false); err != nil {
		t.Fatal(err)
	}
	// NickServ: an open PM, but a service nick — must not be auto-monitored.
	if _, err := c.storage.GetOrCreatePMConversation(nid, "NickServ", self); err != nil {
		t.Fatal(err)
	}

	for _, nick := range []string{"alice", "bob", "carol", "NickServ", self} {
		c.MonitorReconcileNick(nick)
	}

	armed := func(n string) bool {
		c.monitorMu.Lock()
		defer c.monitorMu.Unlock()
		return c.monitorArmed[strings.ToLower(n)]
	}
	if !armed("alice") {
		t.Error("durable buddy alice should be armed")
	}
	if !armed("bob") {
		t.Error("open-PM bob should be armed")
	}
	if armed("carol") {
		t.Error("closed-PM carol should not be armed")
	}
	if armed("NickServ") {
		t.Error("service nick NickServ should not be auto-monitored even with an open PM")
	}
	if armed(self) {
		t.Error("self should never be monitored")
	}
}

func TestSendInitialMonitorArmsUnion(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.supportsMonitor = true
	nid := c.networkID
	self := c.network.Nickname

	if err := c.storage.AddMonitoredNick(nid, "alice"); err != nil { // buddy
		t.Fatal(err)
	}
	if _, err := c.storage.GetOrCreatePMConversation(nid, "bob", self); err != nil { // open PM
		t.Fatal(err)
	}
	if _, err := c.storage.GetOrCreatePMConversation(nid, "NickServ", self); err != nil { // service PM (excluded)
		t.Fatal(err)
	}

	c.sendInitialMonitor()

	c.monitorMu.Lock()
	defer c.monitorMu.Unlock()
	if !c.monitorArmed["alice"] || !c.monitorArmed["bob"] {
		t.Errorf("buddy + open-PM should be armed on connect: %v", c.monitorArmed)
	}
	if c.monitorArmed["nickserv"] {
		t.Error("service nick must not be armed even with an open PM")
	}
}

func TestSendInitialMonitorRespectsLimit(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.supportsMonitor = true
	c.monitorLimit = 2
	nid := c.networkID
	self := c.network.Nickname

	// Two buddies plus an open PM; the limit of 2 must keep both buddies and
	// drop the PM correspondent (buddies are armed first).
	if err := c.storage.AddMonitoredNick(nid, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := c.storage.AddMonitoredNick(nid, "dave"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.storage.GetOrCreatePMConversation(nid, "bob", self); err != nil {
		t.Fatal(err)
	}

	c.sendInitialMonitor()

	c.monitorMu.Lock()
	defer c.monitorMu.Unlock()
	if len(c.monitorArmed) != 2 || !c.monitorArmed["alice"] || !c.monitorArmed["dave"] {
		t.Errorf("limit=2 should arm the two buddies and drop the PM: %v", c.monitorArmed)
	}
}

func TestParseMonitorLimit(t *testing.T) {
	cases := []struct {
		param     string
		wantLimit int
		wantOK    bool
	}{
		{"MONITOR", 0, true},       // supported, no advertised limit
		{"MONITOR=100", 100, true}, // supported with a limit
		{"MONITOR=", 0, true},      // supported, empty limit
		{"MONITOR=abc", 0, true},   // malformed limit → supported, unlimited
		{"MONITORX", 0, false},     // not the MONITOR token
		{"PREFIX=(ov)@+", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.param, func(t *testing.T) {
			limit, ok := parseMonitorLimit(tc.param)
			if ok != tc.wantOK || limit != tc.wantLimit {
				t.Errorf("parseMonitorLimit(%q) = (%d, %v), want (%d, %v)",
					tc.param, limit, ok, tc.wantLimit, tc.wantOK)
			}
		})
	}
}

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
