package cascade

import "testing"

func TestTextEventReplyRoutesToSink(t *testing.T) {
	var got []string
	e := NewTextEvent("alice", "!hello there", func(s string) { got = append(got, s) })

	if !e.HasPrefix("!hello") {
		t.Fatalf("HasPrefix(!hello) = false, want true")
	}
	if e.Arg(1) != "!hello" || e.Arg(2) != "there" {
		t.Fatalf("Arg(1),Arg(2) = %q,%q; want %q,%q", e.Arg(1), e.Arg(2), "!hello", "there")
	}
	if e.Arg(99) != "" {
		t.Fatalf("Arg(99) = %q; want empty", e.Arg(99))
	}

	e.Reply("hi alice")
	if len(got) != 1 || got[0] != "hi alice" {
		t.Fatalf("reply sink = %v; want [hi alice]", got)
	}
}
