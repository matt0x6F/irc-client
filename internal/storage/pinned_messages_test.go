package storage

import (
	"testing"
	"time"
)

// writeMessagesForPinTest writes n channel messages with strictly increasing
// timestamps and returns them (with their assigned DB ids) in chronological order.
func writeMessagesForPinTest(t *testing.T, s *Storage, netID int64, chID *int64, n int) []Message {
	t.Helper()
	base := time.Now().Add(-time.Duration(n) * time.Minute)
	for i := 0; i < n; i++ {
		msg := Message{
			NetworkID:   netID,
			ChannelID:   chID,
			User:        "sender",
			Message:     "msg",
			MessageType: "privmsg",
			Timestamp:   base.Add(time.Duration(i) * time.Second),
		}
		if err := s.WriteMessageSync(msg); err != nil {
			t.Fatalf("WriteMessageSync %d: %v", i, err)
		}
	}
	msgs, err := s.GetMessages(netID, chID, n+10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != n {
		t.Fatalf("expected %d messages, got %d", n, len(msgs))
	}
	return msgs
}

func TestPinUnpinAndGetPinnedMessages(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("PinNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#pins", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	msgs := writeMessagesForPinTest(t, s, net.ID, &ch.ID, 6)
	target := msgs[3].ID

	// Pin a message.
	if err := s.PinMessage(target, net.ID, &ch.ID, "testuser"); err != nil {
		t.Fatalf("PinMessage: %v", err)
	}

	// Pinning again is idempotent (ON CONFLICT DO NOTHING).
	if err := s.PinMessage(target, net.ID, &ch.ID, "someoneelse"); err != nil {
		t.Fatalf("PinMessage (idempotent): %v", err)
	}

	pins, err := s.GetPinnedMessages(net.ID, &ch.ID)
	if err != nil {
		t.Fatalf("GetPinnedMessages: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("expected 1 pinned message, got %d", len(pins))
	}
	if pins[0].ID != target {
		t.Errorf("expected pinned id %d, got %d", target, pins[0].ID)
	}
	if pins[0].PinnedBy != "testuser" {
		t.Errorf("expected pinned_by 'testuser' (first pin wins), got %q", pins[0].PinnedBy)
	}

	// Unpin removes it.
	if err := s.UnpinMessage(target); err != nil {
		t.Fatalf("UnpinMessage: %v", err)
	}
	pins, err = s.GetPinnedMessages(net.ID, &ch.ID)
	if err != nil {
		t.Fatalf("GetPinnedMessages after unpin: %v", err)
	}
	if len(pins) != 0 {
		t.Fatalf("expected 0 pinned messages after unpin, got %d", len(pins))
	}
}

func TestGetMessagesBefore(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("BeforeNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#before", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// 10 messages in chronological order (msgs[0] oldest, msgs[9] newest).
	msgs := writeMessagesForPinTest(t, s, net.ID, &ch.ID, 10)

	// Page strictly older than msgs[5], limit 3 → expect msgs[2], [3], [4] ascending.
	page, err := s.GetMessagesBefore(net.ID, &ch.ID, msgs[5].ID, 3)
	if err != nil {
		t.Fatalf("GetMessagesBefore: %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(page))
	}
	for i, want := range []Message{msgs[2], msgs[3], msgs[4]} {
		if page[i].ID != want.ID {
			t.Errorf("page[%d]: expected id %d, got %d", i, want.ID, page[i].ID)
		}
	}

	// The target id itself must be excluded (strictly before).
	for _, m := range page {
		if m.ID == msgs[5].ID {
			t.Errorf("GetMessagesBefore must exclude the boundary id %d", msgs[5].ID)
		}
	}

	// Nothing older than the very first message.
	empty, err := s.GetMessagesBefore(net.ID, &ch.ID, msgs[0].ID, 5)
	if err != nil {
		t.Fatalf("GetMessagesBefore (oldest): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 messages before the oldest, got %d", len(empty))
	}
}

func TestGetMessagesAround(t *testing.T) {
	s := newTestStorage(t)
	net := makeNetwork("AroundNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#around", CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// 11 messages; target is the middle one (index 5).
	msgs := writeMessagesForPinTest(t, s, net.ID, &ch.ID, 11)
	target := msgs[5].ID

	// window=2 → up to 2 at/before target (incl. target) + up to 2 after.
	around, err := s.GetMessagesAround(net.ID, &ch.ID, target, 2)
	if err != nil {
		t.Fatalf("GetMessagesAround: %v", err)
	}

	// Expect messages at indices 4,5,6,7 (id<=target gives 5 and 4; id>target gives 6 and 7).
	if len(around) != 4 {
		t.Fatalf("expected 4 messages in window, got %d", len(around))
	}

	// Must be ascending by id and include the target.
	foundTarget := false
	for i, m := range around {
		if i > 0 && around[i-1].ID >= m.ID {
			t.Errorf("window not strictly ascending by id at index %d", i)
		}
		if m.ID == target {
			foundTarget = true
		}
	}
	if !foundTarget {
		t.Error("expected the target message to be included in the window")
	}
	if around[0].ID != msgs[4].ID || around[len(around)-1].ID != msgs[7].ID {
		t.Errorf("expected window [%d..%d], got [%d..%d]",
			msgs[4].ID, msgs[7].ID, around[0].ID, around[len(around)-1].ID)
	}
}
