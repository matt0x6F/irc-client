package script

import (
	"maps"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/cascade"
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

func TestBuildTextEventUsesHostChannelClassification(t *testing.T) {
	m := &Manager{host: Host{
		SelfNick:  func(int64) string { return "Matt" },
		IsChannel: func(_ int64, target string) bool { return target == "!room" },
	}}
	te, ok := m.buildTextEvent(events.Event{Data: map[string]interface{}{
		"user": "alice", "channel": "!room", "message": "hello", "networkId": int64(1),
	}})
	if !ok || te.IsDM() {
		t.Fatalf("nonstandard channel: ok=%v dm=%v", ok, te.IsDM())
	}
}

func TestBuildJoinPartEventContext(t *testing.T) {
	m := &Manager{host: Host{
		SelfNick: func(int64) string { return "Matt" },
		UserStatus: func(_ int64, _ string) cascade.UserStatus {
			return cascade.UserStatus{Known: true, Account: "alice_account", Host: "a@host", Realname: "Alice"}
		},
	}}
	ts := time.Unix(42, 0)
	base := map[string]interface{}{
		"networkId": int64(1), "networkName": "Libera", "channel": "#go", "user": "alice",
	}
	je, ok := m.buildJoinEvent(events.Event{Data: base, Timestamp: ts})
	if !ok || je.Self != "Matt" || je.Network != "Libera" || je.Account != "alice_account" || je.Host != "a@host" || je.Realname != "Alice" || je.Time.Unix() != 42 {
		t.Fatalf("join context: ok=%v event=%+v", ok, je)
	}
	partData := maps.Clone(base)
	partData["reason"] = "bye"
	pe, ok := m.buildPartEvent(events.Event{Data: partData, Timestamp: ts})
	if !ok || pe.Self != "Matt" || pe.Network != "Libera" || pe.Account != "alice_account" || pe.Host != "a@host" || pe.Realname != "Alice" || pe.Time.Unix() != 42 {
		t.Fatalf("part context: ok=%v event=%+v", ok, pe)
	}
}
