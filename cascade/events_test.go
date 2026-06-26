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
