package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
)

func TestActivitySettings_DefaultsForMissingTypeToggles(t *testing.T) {
	// Old settings persisted before the type toggles existed: keys absent.
	raw := `{"highlights":true,"keywords":true,"invites":true,"pms":true,"keywordList":[]}`
	s := defaultActivitySettings()
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !s.Notices || !s.Privmsgs {
		t.Fatalf("missing type toggles must default true, got notices=%v privmsgs=%v", s.Notices, s.Privmsgs)
	}

	// Explicit false is honored.
	raw2 := `{"notices":false,"privmsgs":true}`
	s2 := defaultActivitySettings()
	_ = json.Unmarshal([]byte(raw2), &s2)
	if s2.Notices {
		t.Fatalf("explicit notices:false must be honored")
	}
}

func TestActivitySettings_ToConfigMapsTypeToggles(t *testing.T) {
	s := ActivitySettings{Notices: false, Privmsgs: true}
	c := s.toConfig()
	if c.Notices || !c.Privmsgs {
		t.Fatalf("toConfig type toggles wrong: %+v", c)
	}
}

func TestActivitySettingsDefaultsAndRoundTrip(t *testing.T) {
	a := newTestApp(t)

	def, err := a.GetActivitySettings()
	if err != nil {
		t.Fatalf("GetActivitySettings: %v", err)
	}
	if !def.Highlights || !def.Invites || !def.PMs || !def.Keywords {
		t.Fatalf("core sources should default on: %+v", def)
	}
	if len(def.KeywordList) != 0 {
		t.Fatalf("keyword list should default empty: %+v", def.KeywordList)
	}

	want := ActivitySettings{Highlights: true, Keywords: true, Invites: false, PMs: true, KeywordList: []string{"deploy", "prod"}}
	if err := a.SetActivitySettings(want); err != nil {
		t.Fatalf("SetActivitySettings: %v", err)
	}
	got, err := a.GetActivitySettings()
	if err != nil {
		t.Fatalf("GetActivitySettings after set: %v", err)
	}
	if got.Invites != false || len(got.KeywordList) != 2 || got.KeywordList[0] != "deploy" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestRecordMessageActivityWritesHighlight(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNet")
	cfg := irc.ActivityConfig{Highlights: true, Keywords: true, PMs: true, Notices: true, Privmsgs: true}

	a.recordMessageActivity(cfg, "matt", net.ID, "#dev", "alice", "matt can you look?", "mid9", "privmsg", false, time.Now())

	items, _ := a.storage.ListActivityItems(50)
	if len(items) != 1 || items[0].SourceType != "highlight" || items[0].Target != "#dev" || items[0].MsgID != "mid9" {
		t.Fatalf("expected one highlight row, got %+v", items)
	}
}

func TestRecordMessageActivityIgnoresChatter(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNet2")
	a.recordMessageActivity(irc.ActivityConfig{Highlights: true, Notices: true, Privmsgs: true}, "matt", net.ID, "#dev", "alice", "unrelated chatter", "m", "privmsg", false, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("chatter must not write a row, got %+v", items)
	}
}

func TestRecordMessageActivityPM(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNet3")
	a.recordMessageActivity(irc.ActivityConfig{PMs: true, Notices: true, Privmsgs: true}, "matt", net.ID, "matt", "bob", "you around?", "m", "privmsg", true, time.Now())
	items, _ := a.storage.ListActivityItems(50)
	if len(items) != 1 || items[0].SourceType != "pm" || items[0].Target != "bob" {
		t.Fatalf("expected one pm row targeting bob, got %+v", items)
	}
}

func TestRecordMessageActivityIgnoresSelfMention(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNetSelfMention")
	a.recordMessageActivity(irc.ActivityConfig{Highlights: true, Notices: true, Privmsgs: true}, "matt", net.ID, "#dev", "matt", "matt testing my own nick", "m", "privmsg", false, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("self-echoed message must not write a row, got %+v", items)
	}
}

func TestRecordMessageActivityIgnoresSelfPMEcho(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNetSelfPM")
	a.recordMessageActivity(irc.ActivityConfig{PMs: true, Notices: true, Privmsgs: true}, "matt", net.ID, "matt", "matt", "note to self", "m", "privmsg", true, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("self echo-message PM must not write a row, got %+v", items)
	}
}

func TestActivityBoundMethods(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "BoundNet")
	cfg := irc.ActivityConfig{Highlights: true, Notices: true, Privmsgs: true}
	a.recordMessageActivity(cfg, "matt", net.ID, "#dev", "alice", "matt ping", "m1", "privmsg", false, time.Now())
	a.recordMessageActivity(cfg, "matt", net.ID, "#dev", "bob", "matt again", "m2", "privmsg", false, time.Now())

	items, err := a.GetActivityItems()
	if err != nil || len(items) != 2 {
		t.Fatalf("GetActivityItems = %d items, err %v", len(items), err)
	}
	if err := a.MarkActivitySeen(items[0].ID); err != nil {
		t.Fatalf("MarkActivitySeen: %v", err)
	}
	if err := a.DismissActivity(items[1].ID); err != nil {
		t.Fatalf("DismissActivity: %v", err)
	}
	remaining, _ := a.GetActivityItems()
	if len(remaining) != 1 || !remaining[0].Seen {
		t.Fatalf("after mark+dismiss want 1 seen item, got %+v", remaining)
	}
	if err := a.ClearAllActivity(); err != nil {
		t.Fatalf("ClearAllActivity: %v", err)
	}
	if remaining, _ = a.GetActivityItems(); len(remaining) != 0 {
		t.Fatalf("ClearAllActivity should empty the inbox, got %+v", remaining)
	}
}

func TestRecordMessageActivity_TypeAndSenderVeto(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "VetoNet")
	cfg := irc.ActivityConfig{PMs: true, Notices: false, Privmsgs: true}

	// Notices off → a service NOTICE PM writes nothing.
	a.recordMessageActivity(cfg, "matt", net.ID, "matt", "ChanServ", "hello", "m", "notice", true, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("notice must be vetoed, got %+v", items)
	}

	// Ignored sender → even a privmsg PM writes nothing.
	cfgOn := irc.ActivityConfig{PMs: true, Notices: true, Privmsgs: true}
	if err := a.storage.AddIgnoredSender(net.ID, "bob"); err != nil {
		t.Fatalf("add ignore: %v", err)
	}
	a.recordMessageActivity(cfgOn, "matt", net.ID, "matt", "bob", "you around?", "m", "privmsg", true, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("ignored sender must be vetoed, got %+v", items)
	}

	// Non-ignored privmsg PM still writes.
	a.recordMessageActivity(cfgOn, "matt", net.ID, "matt", "alice", "hi", "m", "privmsg", true, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 1 || items[0].Actor != "alice" {
		t.Fatalf("expected one row from alice, got %+v", items)
	}
}
