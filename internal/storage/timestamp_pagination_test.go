package storage

import (
	"testing"
	"time"
)

// TestScrollbackRecoversAllMessagesAcrossTimezones reproduces the scroll-to-top
// stall caused by persisting messages with mixed timezone offsets.
//
// In production, live privmsgs carry an IRCv3 server-time tag and are stored in
// UTC ("...+00:00"), while join/quit/part/mode/echo rows use time.Now() and are
// stored in local wall-clock ("...-07:00"/"-08:00"). SQLite has no native
// datetime type: the TIMESTAMP column is text and `WHERE timestamp < ? ORDER BY
// timestamp DESC` compares it lexicographically. Mixed offsets make text order
// disagree with chronological order, so the timestamp-cursor pager
// (GetMessagesBeforeTime) eventually returns a page of only already-seen rows;
// the frontend reads that as "no more history" and latches reachedStart, leaving
// older messages permanently unreachable.
//
// The fix normalizes every write to UTC, restoring a single lexicographically
// monotonic format. This test walks scrollback exactly like the frontend's
// loadOlderMessages and asserts every inserted message is reachable.
func TestScrollbackRecoversAllMessagesAcrossTimezones(t *testing.T) {
	s := newTestStorage(t)
	networkID, channelID := testChannel(t, s)

	// A fixed western offset mirrors the local-time writes (-07:00 in summer).
	// Using FixedZone keeps the test deterministic and tzdata-independent while
	// reproducing the exact text-format inversion of a real local timezone.
	west := time.FixedZone("PDT", -7*3600)
	base := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	const total = 120
	for i := 0; i < total; i++ {
		// One message per minute, strictly chronologically increasing.
		ts := base.Add(time.Duration(i) * time.Minute)
		// Alternate the source location exactly like production: even = UTC
		// (server-time privmsg), odd = local wall-clock (time.Now() events).
		if i%2 == 1 {
			ts = ts.In(west)
		}
		if err := s.WriteMessageSync(Message{
			NetworkID:   networkID,
			ChannelID:   &channelID,
			User:        "u",
			Message:     "m",
			MessageType: "privmsg",
			Timestamp:   ts,
		}); err != nil {
			t.Fatalf("WriteMessageSync[%d]: %v", i, err)
		}
	}

	// Walk scrollback like the frontend: load the newest page, then repeatedly
	// page by the oldest loaded message's timestamp, stopping when a page yields
	// no new rows (this is the point where the frontend latches reachedStart).
	const page = 25
	seen := map[int64]bool{}
	var oldest *Message

	merge := func(msgs []Message) int {
		fresh := 0
		for i := range msgs {
			m := msgs[i]
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			fresh++
			if oldest == nil || m.Timestamp.Before(oldest.Timestamp) {
				cp := m
				oldest = &cp
			}
		}
		return fresh
	}

	newest, err := s.GetMessages(networkID, &channelID, page)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	merge(newest)

	for iter := 0; iter < total; iter++ { // bounded to guarantee termination
		if oldest == nil {
			break
		}
		older, err := s.GetMessagesBeforeTime(networkID, &channelID, "", oldest.Timestamp, page)
		if err != nil {
			t.Fatalf("GetMessagesBeforeTime: %v", err)
		}
		if merge(older) == 0 {
			break // frontend would latch reachedStart here
		}
	}

	if len(seen) != total {
		t.Fatalf("scrollback stalled: reachable=%d, want all %d messages "+
			"(older rows unreachable due to mixed-timezone lexicographic ordering)",
			len(seen), total)
	}
}

// TestMigrateNormalizesLegacyMixedTimezoneTimestamps verifies the one-time data
// migration that rewrites pre-fix rows (stored with mixed local/UTC offsets) into
// the single canonical UTC text format the scrollback cursor depends on. It must
// convert by instant (not merely relabel) and be idempotent.
func TestMigrateNormalizesLegacyMixedTimezoneTimestamps(t *testing.T) {
	s := newTestStorage(t)
	networkID, channelID := testChannel(t, s)

	// Simulate legacy rows written before the fix: raw INSERTs with mixed offsets,
	// bypassing normalizeForStore (which now forces UTC on every Go write path).
	legacy := []string{
		"2026-06-15 14:00:00.000-07:00", // local PDT -> 21:00 UTC
		"2026-06-15 20:30:00.000+00:00", // already UTC, unchanged
		"2026-01-10 18:00:00.000-08:00", // local PST -> 2026-01-11 02:00 UTC
	}
	for i, ts := range legacy {
		if _, err := s.db.Exec(
			`INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp)
			 VALUES (?, ?, 'u', 'm', 'privmsg', ?)`, networkID, channelID, ts); err != nil {
			t.Fatalf("insert legacy[%d]: %v", i, err)
		}
	}

	// Run the full migration set (idempotent; NewStorage already ran it once).
	if err := Migrate(s.db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var nonUTC int
	if err := s.db.Get(&nonUTC,
		"SELECT COUNT(*) FROM messages WHERE timestamp NOT LIKE '%+00:00'"); err != nil {
		t.Fatalf("count non-UTC: %v", err)
	}
	if nonUTC != 0 {
		t.Fatalf("after migration, %d row(s) still non-UTC", nonUTC)
	}

	// Conversion must be by instant: 14:00 -07:00 becomes 21:00 UTC, not 14:00 UTC.
	var pdt int
	if err := s.db.Get(&pdt,
		"SELECT COUNT(*) FROM messages WHERE timestamp LIKE '2026-06-15 21:00:00%+00:00'"); err != nil {
		t.Fatalf("query converted PDT row: %v", err)
	}
	if pdt != 1 {
		t.Fatalf("PDT row not converted to 21:00 UTC (matches=%d)", pdt)
	}
	// PST row crosses midnight: 2026-01-10 18:00 -08:00 -> 2026-01-11 02:00 UTC.
	var pst int
	if err := s.db.Get(&pst,
		"SELECT COUNT(*) FROM messages WHERE timestamp LIKE '2026-01-11 02:00:00%+00:00'"); err != nil {
		t.Fatalf("query converted PST row: %v", err)
	}
	if pst != 1 {
		t.Fatalf("PST row not converted to 2026-01-11 02:00 UTC (matches=%d)", pst)
	}
}
