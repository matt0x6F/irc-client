// invitestore_test.go
package main

import (
	"testing"
	"time"
)

func fixedClock(t *time.Time) func() time.Time { return func() time.Time { return *t } }

func TestInviteStore_AddDedupAndList(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	s := newInviteStore(fixedClock(&now), func() time.Duration { return 24 * time.Hour })

	if ok := s.add(1, "alice", "#a", true); !ok {
		t.Fatal("expected add to succeed")
	}
	now = now.Add(time.Minute)
	s.add(1, "alice", "#a", true) // dedup: same (sender, channel)
	s.add(1, "alice", "#b", false)

	got := s.list(1)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].Channel != "#b" { // newest-first
		t.Fatalf("want #b first, got %q", got[0].Channel)
	}
}

func TestInviteStore_TTLExpiry(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	s := newInviteStore(fixedClock(&now), func() time.Duration { return time.Hour })
	s.add(1, "alice", "#a", true)
	now = now.Add(2 * time.Hour)
	if len(s.list(1)) != 0 {
		t.Fatal("expected entry expired past TTL")
	}
	changed := s.sweep()
	if len(changed) != 1 || changed[0] != 1 {
		t.Fatalf("expected sweep to report network 1, got %v", changed)
	}
}

func TestInviteStore_PerSenderCap(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	s := newInviteStore(fixedClock(&now), func() time.Duration { return 24 * time.Hour })
	for i := 0; i < maxChannelsPerSender+5; i++ {
		now = now.Add(time.Second)
		s.add(1, "flood", "#c"+time.Duration(i).String(), false)
	}
	if got := s.count(1); got != maxChannelsPerSender {
		t.Fatalf("want cap %d, got %d", maxChannelsPerSender, got)
	}
}

func TestInviteStore_IgnoreSenderBlocksFuture(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	s := newInviteStore(fixedClock(&now), func() time.Duration { return 24 * time.Hour })
	s.add(1, "troll", "#x", false)
	s.ignoreSender(1, "troll")
	if s.count(1) != 0 {
		t.Fatal("ignoreSender should remove existing invites")
	}
	if ok := s.add(1, "TROLL", "#y", false); ok {
		t.Fatal("blocked sender (case-insensitive) should be dropped")
	}
}
