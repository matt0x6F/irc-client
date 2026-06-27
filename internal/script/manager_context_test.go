package script

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
)

func TestBuildTextEventContext(t *testing.T) {
	m := &Manager{host: Host{
		SelfNick: func(id int64) string { return "matt0x6f" },
		Send:     func(int64, string, string) error { return nil },
	}}
	ev := events.Event{Data: map[string]interface{}{
		"user":        "alittlefang",
		"channel":     "#chan",
		"message":     "matt0x6f hello",
		"networkId":   int64(1),
		"networkName": "Libera",
		"account":     "fang_acct",
		"msgid":       "xyz789",
		"messageUnix": int64(1750000000),
		"isAction":    true,
	}}
	te, ok := m.buildTextEvent(ev)
	if !ok {
		t.Fatal("buildTextEvent returned ok=false")
	}
	if te.Self != "matt0x6f" || !te.IsHighlight() {
		t.Fatalf("Self/IsHighlight wrong: self=%q hl=%v", te.Self, te.IsHighlight())
	}
	if te.Account != "fang_acct" || te.Network != "Libera" || te.MsgID != "xyz789" {
		t.Fatalf("context fields wrong: %+v", te)
	}
	if te.Time.Unix() != 1750000000 || !te.Action {
		t.Fatalf("time/action wrong: unix=%d action=%v", te.Time.Unix(), te.Action)
	}
}
