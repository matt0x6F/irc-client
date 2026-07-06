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

func TestInviteActivityColumnsAndQueries(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("InvNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	now := time.Now().Truncate(time.Second)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	// live invite (expires in the future) + expired invite from same sender
	if _, err := s.WriteActivityItem(ActivityItem{NetworkID: net.ID, SourceType: "invite", Target: "#ops", Actor: "carol", Trusted: true, ExpiresAt: &future, Timestamp: now}); err != nil {
		t.Fatalf("write live invite: %v", err)
	}
	if _, err := s.WriteActivityItem(ActivityItem{NetworkID: net.ID, SourceType: "invite", Target: "#old", Actor: "carol", ExpiresAt: &past, Timestamp: past}); err != nil {
		t.Fatalf("write expired invite: %v", err)
	}

	live, err := s.ListInviteActivity(net.ID, now)
	if err != nil {
		t.Fatalf("ListInviteActivity: %v", err)
	}
	if len(live) != 1 || live[0].Target != "#ops" || !live[0].Trusted {
		t.Fatalf("expected one live trusted invite, got %+v", live)
	}

	nets, err := s.SweepExpiredInvites(now)
	if err != nil {
		t.Fatalf("SweepExpiredInvites: %v", err)
	}
	if len(nets) != 1 || nets[0] != net.ID {
		t.Fatalf("sweep should report net %d, got %v", net.ID, nets)
	}
	// only the expired one is gone; the live one remains
	if live, _ = s.ListInviteActivity(net.ID, now); len(live) != 1 {
		t.Fatalf("sweep should keep the live invite, got %+v", live)
	}

	if err := s.DeleteInviteActivity(net.ID, "carol", "#ops"); err != nil {
		t.Fatalf("DeleteInviteActivity: %v", err)
	}
	if live, _ = s.ListInviteActivity(net.ID, now); len(live) != 0 {
		t.Fatalf("delete should remove the invite, got %+v", live)
	}
}

func TestInviteActivityCaseInsensitiveMatch(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("InvCaseNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	now := time.Now().Truncate(time.Second)

	// DeleteInviteActivity must match actor/target case-insensitively.
	if _, err := s.WriteActivityItem(ActivityItem{NetworkID: net.ID, SourceType: "invite", Target: "#Ops", Actor: "Alice", Timestamp: now}); err != nil {
		t.Fatalf("write invite: %v", err)
	}
	if err := s.DeleteInviteActivity(net.ID, "alice", "#ops"); err != nil {
		t.Fatalf("DeleteInviteActivity: %v", err)
	}
	if live, err := s.ListInviteActivity(net.ID, now); err != nil {
		t.Fatalf("ListInviteActivity: %v", err)
	} else if len(live) != 0 {
		t.Fatalf("expected case-insensitive delete to remove the invite, got %+v", live)
	}

	// DeleteInviteActivityFromSender must match actor case-insensitively across rows.
	if _, err := s.WriteActivityItem(ActivityItem{NetworkID: net.ID, SourceType: "invite", Target: "#x", Actor: "Bob", Timestamp: now}); err != nil {
		t.Fatalf("write invite (Bob/#x): %v", err)
	}
	if _, err := s.WriteActivityItem(ActivityItem{NetworkID: net.ID, SourceType: "invite", Target: "#y", Actor: "BOB", Timestamp: now}); err != nil {
		t.Fatalf("write invite (BOB/#y): %v", err)
	}
	if err := s.DeleteInviteActivityFromSender(net.ID, "bob"); err != nil {
		t.Fatalf("DeleteInviteActivityFromSender: %v", err)
	}
	if live, err := s.ListInviteActivity(net.ID, now); err != nil {
		t.Fatalf("ListInviteActivity: %v", err)
	} else if len(live) != 0 {
		t.Fatalf("expected case-insensitive DeleteInviteActivityFromSender to remove both rows, got %+v", live)
	}
}
