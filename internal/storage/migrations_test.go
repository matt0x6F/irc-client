package storage

import "testing"

// TestMigrateReplyAndContextAddsColumns verifies that migrateReplyAndContext
// adds the reply_msgid and channel_context columns to an existing messages table
// and is idempotent (a second call must not error).
func TestMigrateReplyAndContextAddsColumns(t *testing.T) {
	s := newTestStorage(t)
	// Simulate a legacy DB: drop the new columns if present.
	_, _ = s.db.Exec("ALTER TABLE messages DROP COLUMN reply_msgid")
	_, _ = s.db.Exec("ALTER TABLE messages DROP COLUMN channel_context")

	if err := migrateReplyAndContext(s.db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	for _, col := range []string{"reply_msgid", "channel_context"} {
		var n int
		if err := s.db.Get(&n, "SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name=?", col); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("expected column %q to exist, got count %d", col, n)
		}
	}
	// Idempotent: second run must not error.
	if err := migrateReplyAndContext(s.db); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
}
