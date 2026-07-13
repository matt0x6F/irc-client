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

// TestMigrateEventDedupIndex verifies that migrateEventDedupIndex replaces the
// legacy coarse (network_id, msgid) unique index with the per-conversation
// idx_messages_conv_msgid index, and that it is idempotent (a second call must
// not error and must leave the new index in place).
func TestMigrateEventDedupIndex(t *testing.T) {
	s := newTestStorage(t)
	// Simulate a legacy DB: drop the new index, recreate the old coarse one.
	_, _ = s.db.Exec(`DROP INDEX IF EXISTS idx_messages_conv_msgid`)
	_, _ = s.db.Exec(`CREATE UNIQUE INDEX idx_messages_network_msgid ON messages(network_id, msgid) WHERE msgid IS NOT NULL`)

	if err := migrateEventDedupIndex(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Idempotent second run.
	if err := migrateEventDedupIndex(s.db); err != nil {
		t.Fatalf("migrate (2nd): %v", err)
	}

	var name string
	err := s.db.Get(&name, `SELECT name FROM sqlite_master WHERE type='index' AND name='idx_messages_conv_msgid'`)
	if err != nil {
		t.Fatalf("new index missing: %v", err)
	}
	var old int
	_ = s.db.Get(&old, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_messages_network_msgid'`)
	if old != 0 {
		t.Fatalf("old index should be dropped")
	}
}

func TestMigratePluginConfigColumnNormalizesLegacyJSON(t *testing.T) {
	s := newTestStorage(t)

	if _, err := s.db.Exec(`
		DROP TABLE plugin_configs;
		CREATE TABLE plugin_configs (
			name TEXT PRIMARY KEY,
			enabled BOOLEAN NOT NULL DEFAULT 1,
			config TEXT,
			config_schema TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO plugin_configs (name, enabled, config, config_schema)
		VALUES ('null-json', 0, NULL, NULL),
		       ('text-json', 1, '{"answer":42}', '{"type":"object"}')
	`); err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}

	if err := migratePluginConfigColumn(s.db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if err := migratePluginConfigColumn(s.db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}

	configs, err := s.GetAllPluginConfigs()
	if err != nil {
		t.Fatalf("GetAllPluginConfigs: %v", err)
	}
	if configs["null-json"].Enabled {
		t.Fatal("migration changed disabled state")
	}
	if len(configs["null-json"].Config) != 0 || len(configs["null-json"].ConfigSchema) != 0 {
		t.Fatalf("NULL JSON was not normalized to empty objects: %+v", configs["null-json"])
	}
	if configs["text-json"].Config["answer"] != float64(42) {
		t.Fatalf("TEXT config was not preserved: %+v", configs["text-json"].Config)
	}
	if configs["text-json"].ConfigSchema["type"] != "object" {
		t.Fatalf("TEXT config schema was not preserved: %+v", configs["text-json"].ConfigSchema)
	}
}
