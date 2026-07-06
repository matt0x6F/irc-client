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
