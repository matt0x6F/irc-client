package main

import "testing"

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
