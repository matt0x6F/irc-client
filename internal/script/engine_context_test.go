package script

import (
	"testing"

	"github.com/matt0x6f/irc-client/cascade"
)

func TestScriptUsesIsHighlight(t *testing.T) {
	// testdata/highlighter is a sandboxed script that calls e.IsHighlight()
	// and replies "world" when the bot nick appears in a message containing "hello".
	s, err := LoadPackage("testdata/highlighter")
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	mk := func(msg string) cascade.TextEvent {
		e := cascade.NewTextEvent("alittlefang", "#chan", msg, nil)
		e.Self = "matt0x6f"
		return e
	}
	var got []string
	reply := func(m string) { got = append(got, m) }

	// Case 1: highlight + "hello" → expect reply "world".
	e1 := mk("matt0x6f hello")
	e1Reply := cascade.NewTextEvent(e1.Nick, e1.Channel, e1.Message, reply)
	e1Reply.Self = "matt0x6f"
	s.DispatchText(e1Reply)
	if len(got) != 1 || got[0] != "world" {
		t.Fatalf("highlight+hello reply = %v; want [world]", got)
	}

	// Case 2: no highlight → expect silence.
	got = nil
	e2 := cascade.NewTextEvent("alittlefang", "#chan", "hello everyone", reply)
	e2.Self = "matt0x6f"
	s.DispatchText(e2)
	if len(got) != 0 {
		t.Fatalf("no-highlight reply = %v; want []", got)
	}
}
