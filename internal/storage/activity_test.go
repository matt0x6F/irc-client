package storage

import (
	"testing"
	"time"
)

func TestActivityItemRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("ActNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	base := time.Now().Truncate(time.Second)
	if _, err := s.WriteActivityItem(ActivityItem{
		NetworkID:  net.ID,
		SourceType: "highlight",
		Target:     "#dev",
		Actor:      "alice",
		Preview:    "can you review #147",
		MsgID:      "m1",
		Timestamp:  base,
	}); err != nil {
		t.Fatalf("WriteActivityItem: %v", err)
	}

	items, err := s.ListActivityItems(50)
	if err != nil {
		t.Fatalf("ListActivityItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	got := items[0]
	if got.SourceType != "highlight" || got.Target != "#dev" || got.Actor != "alice" ||
		got.MsgID != "m1" || got.Seen {
		t.Fatalf("unexpected item: %+v", got)
	}
}

func TestActivityItemOrderingAndState(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("ActNet2")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	base := time.Now().Truncate(time.Second)

	older, err := s.WriteActivityItem(ActivityItem{NetworkID: net.ID, SourceType: "pm", Target: "bob", Actor: "bob", Preview: "ping", Timestamp: base.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("write older: %v", err)
	}
	newer, err := s.WriteActivityItem(ActivityItem{NetworkID: net.ID, SourceType: "invite", Target: "#ops", Actor: "carol", Preview: "invited you", Timestamp: base})
	if err != nil {
		t.Fatalf("write newer: %v", err)
	}

	items, _ := s.ListActivityItems(50)
	if len(items) != 2 || items[0].ID != newer.ID || items[1].ID != older.ID {
		t.Fatalf("want newest-first ordering, got %+v", items)
	}

	if err := s.MarkActivitySeen(older.ID); err != nil {
		t.Fatalf("MarkActivitySeen: %v", err)
	}
	items, _ = s.ListActivityItems(50)
	for _, it := range items {
		if it.ID == older.ID && !it.Seen {
			t.Fatalf("expected item %d to be seen", older.ID)
		}
		if it.ID == newer.ID && it.Seen {
			t.Fatalf("expected item %d to stay unseen", newer.ID)
		}
	}

	if err := s.ClearSeenActivity(); err != nil {
		t.Fatalf("ClearSeenActivity: %v", err)
	}
	items, _ = s.ListActivityItems(50)
	if len(items) != 1 || items[0].ID != newer.ID {
		t.Fatalf("clear-seen should leave only the unseen item, got %+v", items)
	}

	if err := s.DismissActivity(newer.ID); err != nil {
		t.Fatalf("DismissActivity: %v", err)
	}
	if items, _ = s.ListActivityItems(50); len(items) != 0 {
		t.Fatalf("expected empty after dismiss, got %+v", items)
	}
}
