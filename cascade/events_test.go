package cascade

import "testing"

func TestTextEventReplyRoutesToSink(t *testing.T) {
	var got []string
	e := NewTextEvent("alice", "#chan", "!hello there", func(s string) { got = append(got, s) })

	if e.Channel != "#chan" || e.IsDM() {
		t.Fatalf("Channel=%q IsDM=%v; want #chan / false", e.Channel, e.IsDM())
	}
	if !e.HasPrefix("!hello") || e.Arg(2) != "there" {
		t.Fatalf("HasPrefix/Arg wrong: %v %q", e.HasPrefix("!hello"), e.Arg(2))
	}
	e.Reply("hi alice")
	if len(got) != 1 || got[0] != "hi alice" {
		t.Fatalf("reply sink = %v; want [hi alice]", got)
	}

	dm := NewTextEvent("bob", "mynick", "yo", nil)
	if !dm.IsDM() {
		t.Fatalf("expected IsDM true for non-channel target %q", dm.Channel)
	}
}

func TestNoticeJoinPartEvents(t *testing.T) {
	var got []string
	sink := func(s string) { got = append(got, s) }

	n := NewNoticeEvent("serv", "#c", "hi", sink)
	if n.Nick != "serv" || n.Channel != "#c" || n.Message != "hi" || n.IsDM() {
		t.Fatalf("notice fields wrong: %+v", n)
	}
	n.Reply("ack")

	j := NewJoinEvent("bob", "#c", sink)
	if j.Nick != "bob" || j.Channel != "#c" {
		t.Fatalf("join fields wrong: %+v", j)
	}
	j.Reply("welcome bob")

	p := NewPartEvent("bob", "#c", "bye", sink)
	if p.Nick != "bob" || p.Channel != "#c" || p.Reason != "bye" {
		t.Fatalf("part fields wrong: %+v", p)
	}
	p.Reply("cya")

	if len(got) != 3 || got[0] != "ack" || got[1] != "welcome bob" || got[2] != "cya" {
		t.Fatalf("replies = %v", got)
	}
}

func TestTime(t *testing.T) {
	z := NewTime(0)
	if !z.IsZero() {
		t.Fatalf("NewTime(0).IsZero() = false; want true")
	}
	a := NewTime(1000)
	b := NewTime(2000)
	if !a.Before(b) || !b.After(a) {
		t.Fatalf("ordering wrong: a=%d b=%d", a.Unix(), b.Unix())
	}
	if a.Unix() != 1000 {
		t.Fatalf("Unix() = %d; want 1000", a.Unix())
	}
	// 1000s past the Unix epoch is 00:16:40 UTC; just assert HH:MM shape.
	if len(b.Clock()) != 5 || b.Clock()[2] != ':' {
		t.Fatalf("Clock() = %q; want HH:MM", b.Clock())
	}
}

func TestTextEventIsHighlight(t *testing.T) {
	mk := func(self, msg string) TextEvent {
		e := NewTextEvent("bob", "#chan", msg, func(string) {})
		e.Self = self
		return e
	}
	cases := []struct {
		self, msg string
		want      bool
	}{
		{"matt0x6f", "matt0x6f hello", true},
		{"matt0x6f", "hey MATT0X6F", true},      // case-insensitive
		{"matt0x6f", "yo matt0x6f, sup", true},  // trailing punctuation = boundary
		{"matt0x6f", "matt0x6f_0 left", false},  // word-boundary: not a substring hit
		{"matt0x6f", "robotmatt0x6fish", false}, // embedded, not a word
		{"", "matt0x6f hello", false},           // no Self set
	}
	for _, c := range cases {
		if got := mk(c.self, c.msg).IsHighlight(); got != c.want {
			t.Fatalf("IsHighlight(self=%q msg=%q) = %v; want %v", c.self, c.msg, got, c.want)
		}
	}
}

func TestTextEventFieldsAreSettable(t *testing.T) {
	e := NewTextEvent("bob", "#chan", "hi", func(string) {})
	e.Self, e.Account, e.Network, e.MsgID = "me", "bob_acct", "Libera", "abc123"
	e.Time, e.Action = NewTime(42), true
	if e.Self != "me" || e.Account != "bob_acct" || e.Network != "Libera" ||
		e.MsgID != "abc123" || e.Time.Unix() != 42 || !e.Action {
		t.Fatalf("exported fields did not round-trip: %+v", e)
	}
}

func TestNoticeEventContext(t *testing.T) {
	e := NewNoticeEvent("serv", "#chan", "ping matt0x6f", func(string) {})
	e.Self, e.Account, e.Network, e.MsgID, e.Time = "matt0x6f", "a", "Libera", "id", NewTime(7)
	if !e.IsHighlight() {
		t.Fatalf("expected highlight")
	}
	if e.Account != "a" || e.Network != "Libera" || e.MsgID != "id" || e.Time.Unix() != 7 {
		t.Fatalf("fields did not round-trip: %+v", e)
	}
}

func TestAuthoritativeDMClassification(t *testing.T) {
	channel := NewTextEventWithDirect("alice", "!room", "hi", false, nil)
	if channel.IsDM() {
		t.Fatal("host-classified nonstandard channel reported as DM")
	}
	dm := NewNoticeEventWithDirect("alice", "Matt", "hi", true, nil)
	if !dm.IsDM() {
		t.Fatal("host-classified nick target did not report DM")
	}
}

func TestJoinPartEventContext(t *testing.T) {
	j := NewJoinEvent("alice", "#go", nil)
	j.Self, j.Account, j.Network, j.Host, j.Realname, j.Time = "Matt", "alice_account", "libera", "a@host", "Alice", NewTime(42)
	if j.Self != "Matt" || j.Account != "alice_account" || j.Network != "libera" || j.Host != "a@host" || j.Realname != "Alice" || j.Time.Unix() != 42 {
		t.Fatalf("join context = %+v", j)
	}
	p := NewPartEvent("alice", "#go", "bye", nil)
	p.Self, p.Account, p.Network, p.Host, p.Realname, p.Time = "Matt", "alice_account", "libera", "a@host", "Alice", NewTime(43)
	if p.Self != "Matt" || p.Account != "alice_account" || p.Network != "libera" || p.Host != "a@host" || p.Realname != "Alice" || p.Time.Unix() != 43 {
		t.Fatalf("part context = %+v", p)
	}
}
