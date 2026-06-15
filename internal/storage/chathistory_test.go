package storage

import (
	"testing"
	"time"
)

// testChannel creates a network + channel and returns their ids for history tests.
func testChannel(t *testing.T, s *Storage) (networkID, channelID int64) {
	t.Helper()
	net := makeNetwork("HistNet")
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	ch := &Channel{NetworkID: net.ID, Name: "#hist", IsOpen: true, CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	return net.ID, ch.ID
}

// TestWriteHistoryMessagesDedup verifies that replayed CHATHISTORY rows are
// deduplicated by msgid against rows already in the store, and that the returned
// count reflects only genuinely-new inserts.
func TestWriteHistoryMessagesDedup(t *testing.T) {
	s := newTestStorage(t)
	networkID, channelID := testChannel(t, s)

	base := time.Now().Add(-time.Hour).UTC()

	// A "live" row already stored, carrying msgid "X".
	live := Message{
		NetworkID:   networkID,
		ChannelID:   &channelID,
		User:        "alice",
		Message:     "hello",
		MessageType: "privmsg",
		Timestamp:   base,
		MsgID:       "X",
	}
	if err := s.WriteMessageDirect(live); err != nil {
		t.Fatalf("WriteMessageDirect: %v", err)
	}

	// A replay batch containing the already-seen "X" plus a new "Y".
	replay := []Message{
		{NetworkID: networkID, ChannelID: &channelID, User: "alice", Message: "hello", MessageType: "privmsg", Timestamp: base, MsgID: "X"},
		{NetworkID: networkID, ChannelID: &channelID, User: "bob", Message: "hi there", MessageType: "privmsg", Timestamp: base.Add(time.Minute), MsgID: "Y"},
	}
	inserted, err := s.WriteHistoryMessages(replay)
	if err != nil {
		t.Fatalf("WriteHistoryMessages: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected 1 new row inserted (Y), got %d", inserted)
	}

	msgs, err := s.GetMessages(networkID, &channelID, 100)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 stored messages after dedup, got %d", len(msgs))
	}

	// A second replay of the same batch must insert nothing (idempotent).
	inserted2, err := s.WriteHistoryMessages(replay)
	if err != nil {
		t.Fatalf("WriteHistoryMessages (2nd): %v", err)
	}
	if inserted2 != 0 {
		t.Fatalf("expected 0 new rows on idempotent replay, got %d", inserted2)
	}
}

// TestWriteHistoryMessagesNoMsgIDNotDeduped verifies that msgid-less rows are
// exempt from the dedup index (they store NULL, which never collides).
func TestWriteHistoryMessagesNoMsgIDNotDeduped(t *testing.T) {
	s := newTestStorage(t)
	networkID, channelID := testChannel(t, s)
	base := time.Now().Add(-time.Hour).UTC()

	rows := []Message{
		{NetworkID: networkID, ChannelID: &channelID, User: "a", Message: "one", MessageType: "privmsg", Timestamp: base},
		{NetworkID: networkID, ChannelID: &channelID, User: "a", Message: "two", MessageType: "privmsg", Timestamp: base.Add(time.Second)},
	}
	inserted, err := s.WriteHistoryMessages(rows)
	if err != nil {
		t.Fatalf("WriteHistoryMessages: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected 2 msgid-less rows inserted, got %d", inserted)
	}
}

// TestGetMessagesBeforeTime verifies timestamp-keyed pagination — crucially that a
// row inserted later (higher id) but with an older timestamp (a CHATHISTORY
// backfill) is still returned by a "before" query keyed on time.
func TestGetMessagesBeforeTime(t *testing.T) {
	s := newTestStorage(t)
	networkID, channelID := testChannel(t, s)

	t1 := time.Date(2024, 6, 14, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	t3 := t2.Add(time.Minute)

	// Insert t1, t2, t3 in chronological order (ascending ids).
	for i, ts := range []time.Time{t1, t2, t3} {
		if _, err := s.WriteHistoryMessages([]Message{{
			NetworkID: networkID, ChannelID: &channelID, User: "u", Message: "m", MessageType: "privmsg",
			Timestamp: ts, MsgID: string(rune('a' + i)),
		}}); err != nil {
			t.Fatalf("seed insert: %v", err)
		}
	}

	// "before t3" must return t1, t2 in chronological order, excluding t3.
	before, err := s.GetMessagesBeforeTime(networkID, &channelID, "", t3, 100)
	if err != nil {
		t.Fatalf("GetMessagesBeforeTime: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("expected 2 messages before t3, got %d", len(before))
	}
	if !before[0].Timestamp.Equal(t1) || !before[1].Timestamp.Equal(t2) {
		t.Fatalf("expected chronological [t1,t2], got [%v,%v]", before[0].Timestamp, before[1].Timestamp)
	}

	// Now backfill an OLDER message t0 (inserted last -> highest id, oldest time).
	t0 := t1.Add(-time.Minute)
	if _, err := s.WriteHistoryMessages([]Message{{
		NetworkID: networkID, ChannelID: &channelID, User: "u", Message: "old", MessageType: "privmsg",
		Timestamp: t0, MsgID: "z",
	}}); err != nil {
		t.Fatalf("backfill insert: %v", err)
	}

	// "before t1" must surface the backfilled t0 despite its higher id — this is
	// the case id-keyed pagination would miss.
	older, err := s.GetMessagesBeforeTime(networkID, &channelID, "", t1, 100)
	if err != nil {
		t.Fatalf("GetMessagesBeforeTime (backfill): %v", err)
	}
	if len(older) != 1 {
		t.Fatalf("expected 1 backfilled message before t1, got %d", len(older))
	}
	if !older[0].Timestamp.Equal(t0) {
		t.Fatalf("expected backfilled t0, got %v", older[0].Timestamp)
	}
}
