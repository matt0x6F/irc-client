package main

import (
	"testing"
)

func newCacheApp() *App {
	return &App{channelListCache: make(map[int64]channelListCacheEntry)}
}

func TestGetCachedChannelList_Miss(t *testing.T) {
	a := newCacheApp()
	res := a.GetCachedChannelList(42)
	if res.Found {
		t.Fatalf("expected Found=false for empty cache, got %+v", res)
	}
	if len(res.Channels) != 0 {
		t.Fatalf("expected no channels on miss, got %d", len(res.Channels))
	}
}

func TestCacheChannelList_StoreAndRead(t *testing.T) {
	a := newCacheApp()
	channels := []map[string]interface{}{
		{"channel": "#go", "users": 10, "topic": "gophers", "networkId": int64(1)},
		{"channel": "#irc", "users": 3, "topic": "chat", "networkId": int64(1)},
	}
	a.cacheChannelList(1, channels)

	res := a.GetCachedChannelList(1)
	if !res.Found {
		t.Fatal("expected Found=true after caching")
	}
	if len(res.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(res.Channels))
	}
	if res.Channels[0]["channel"] != "#go" {
		t.Fatalf("unexpected first channel: %v", res.Channels[0]["channel"])
	}
	if res.FetchedAt <= 0 {
		t.Fatalf("expected positive FetchedAt, got %d", res.FetchedAt)
	}
}

func TestCacheChannelList_Overwrite(t *testing.T) {
	a := newCacheApp()
	a.cacheChannelList(1, []map[string]interface{}{{"channel": "#old"}})
	first := a.GetCachedChannelList(1)

	a.cacheChannelList(1, []map[string]interface{}{
		{"channel": "#new-a"},
		{"channel": "#new-b"},
	})
	second := a.GetCachedChannelList(1)

	if len(second.Channels) != 2 {
		t.Fatalf("expected overwrite to 2 channels, got %d", len(second.Channels))
	}
	if second.Channels[0]["channel"] != "#new-a" {
		t.Fatalf("expected overwritten data, got %v", second.Channels[0]["channel"])
	}
	if second.FetchedAt < first.FetchedAt {
		t.Fatalf("expected FetchedAt to advance on overwrite: first=%d second=%d", first.FetchedAt, second.FetchedAt)
	}
}

func TestChannelsFromEventData(t *testing.T) {
	// Matches the []interface{} of map[string]interface{} shape emitted on LISTEND.
	raw := []interface{}{
		map[string]interface{}{"channel": "#a", "users": 1},
		map[string]interface{}{"channel": "#b", "users": 2},
		"not-a-map", // ignored
	}
	got := channelsFromEventData(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 valid channels, got %d", len(got))
	}
	if got[1]["channel"] != "#b" {
		t.Fatalf("unexpected channel: %v", got[1]["channel"])
	}

	if channelsFromEventData(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
	if channelsFromEventData("garbage") != nil {
		t.Fatal("expected nil for non-slice input")
	}
}
