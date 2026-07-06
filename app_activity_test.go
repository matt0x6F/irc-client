package main

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
)

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
	cfg := irc.ActivityConfig{Highlights: true, Keywords: true, PMs: true}

	a.recordMessageActivity(cfg, "matt", net.ID, "#dev", "alice", "matt can you look?", "mid9", false, time.Now())

	items, _ := a.storage.ListActivityItems(50)
	if len(items) != 1 || items[0].SourceType != "highlight" || items[0].Target != "#dev" || items[0].MsgID != "mid9" {
		t.Fatalf("expected one highlight row, got %+v", items)
	}
}

func TestRecordMessageActivityIgnoresChatter(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNet2")
	a.recordMessageActivity(irc.ActivityConfig{Highlights: true}, "matt", net.ID, "#dev", "alice", "unrelated chatter", "m", false, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("chatter must not write a row, got %+v", items)
	}
}

func TestRecordMessageActivityPM(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNet3")
	a.recordMessageActivity(irc.ActivityConfig{PMs: true}, "matt", net.ID, "matt", "bob", "you around?", "m", true, time.Now())
	items, _ := a.storage.ListActivityItems(50)
	if len(items) != 1 || items[0].SourceType != "pm" || items[0].Target != "bob" {
		t.Fatalf("expected one pm row targeting bob, got %+v", items)
	}
}

func TestRecordMessageActivityIgnoresSelfMention(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNetSelfMention")
	a.recordMessageActivity(irc.ActivityConfig{Highlights: true}, "matt", net.ID, "#dev", "matt", "matt testing my own nick", "m", false, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("self-echoed message must not write a row, got %+v", items)
	}
}

func TestRecordMessageActivityIgnoresSelfPMEcho(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "RecNetSelfPM")
	a.recordMessageActivity(irc.ActivityConfig{PMs: true}, "matt", net.ID, "matt", "matt", "note to self", "m", true, time.Now())
	if items, _ := a.storage.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("self echo-message PM must not write a row, got %+v", items)
	}
}

func TestActivityBoundMethods(t *testing.T) {
	a := newTestApp(t)
	net := makeAppTestNetwork(t, a.storage, "BoundNet")
	cfg := irc.ActivityConfig{Highlights: true}
	a.recordMessageActivity(cfg, "matt", net.ID, "#dev", "alice", "matt ping", "m1", false, time.Now())
	a.recordMessageActivity(cfg, "matt", net.ID, "#dev", "bob", "matt again", "m2", false, time.Now())

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
