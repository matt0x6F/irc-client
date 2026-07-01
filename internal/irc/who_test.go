package irc

import "testing"

// A user-initiated /who surfaces each 352 row and a closing footer to the status
// buffer, and clears the pending marker.
func TestUserWhoReplySurfacesRowsAndFooter(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// RequestWho marks the target pending; the SendRaw is a no-op with no conn.
	if err := c.RequestWho("#test"); err != nil {
		t.Fatalf("RequestWho: %v", err)
	}

	c.handleWhoReply(parse(t, ":srv 352 me #test ~user host.example irc.srv alice H@ :0 Alice Liddell"))
	c.handleEndOfWho(parse(t, ":srv 315 me #test :End of /WHO list"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	if !hasStatusLine(msgs, "status", "alice (H@) ~user@host.example on #test — Alice Liddell") {
		t.Fatalf("missing formatted WHO row; got %+v", msgs)
	}
	if !hasStatusLine(msgs, "status", "End of WHO for #test") {
		t.Fatalf("missing WHO footer; got %+v", msgs)
	}

	c.whoMu.Lock()
	still := c.whoPending["#test"]
	c.whoMu.Unlock()
	if still {
		t.Fatalf("pending marker not cleared after 315")
	}
}

// Roster-seed WHOX replies (no user /who pending) must stay silent: a 352 with no
// pending target is ignored, and the WHOX 315 that terminates a seed writes nothing.
func TestRosterSeedWhoStaysSilent(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// No RequestWho -> nothing pending (simulates the automatic join-time WHOX).
	c.handleWhoReply(parse(t, ":srv 352 me #seed ~u h.example irc.srv bob G :0 Bob"))
	c.handleEndOfWho(parse(t, ":srv 315 me #seed :End of /WHO list"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	for _, m := range msgs {
		if m.MessageType == "status" {
			t.Fatalf("roster-seed WHO reply leaked to status: %+v", m)
		}
	}
}

// The rfc1459 fold applies to WHO correlation too: /who Nick[a] must match the
// server's 315 echo of the equivalent nick{a}.
func TestUserWhoCorrelationFoldsMask(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	setCaseMapping(c, "rfc1459")

	if err := c.RequestWho("Nick[a]"); err != nil {
		t.Fatalf("RequestWho: %v", err)
	}
	c.handleEndOfWho(parse(t, ":srv 315 me nick{a} :End of /WHO list"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	if !hasStatusLine(msgs, "status", "End of WHO for nick{a}") {
		t.Fatalf("rfc1459 mask fold failed to correlate 315; got %+v", msgs)
	}
}

func TestFormatWhoReply(t *testing.T) {
	got := formatWhoReply("#chan", "~user", "host.example", "irc.srv", "alice", "H@", "Alice Liddell")
	want := "alice (H@) ~user@host.example on #chan — Alice Liddell"
	if got != want {
		t.Fatalf("formatWhoReply = %q, want %q", got, want)
	}
}
