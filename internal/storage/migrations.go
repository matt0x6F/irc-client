package storage

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Migrate runs all database migrations
func Migrate(db *sqlx.DB) error {
	// First, check if we need to run the refactoring migration
	if err := migrateRefactorTables(db); err != nil {
		return fmt.Errorf("refactoring migration failed: %w", err)
	}

	migrations := []string{
		createNetworksTable,
		createServersTable,
		createChannelsTable,
		createChannelUsersTable,
		createMessagesTable,
		createIndexes,
	}

	for i, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}

	// Handle SASL fields migration separately (check if columns exist first)
	if err := migrateSASLFields(db); err != nil {
		return fmt.Errorf("SASL migration failed: %w", err)
	}

	// Handle channel info migration (add topic, modes, updated_at if they don't exist)
	if err := migrateChannelInfo(db); err != nil {
		return fmt.Errorf("channel info migration failed: %w", err)
	}

	// Handle auto_connect field migration
	if err := migrateAutoConnect(db); err != nil {
		return fmt.Errorf("auto_connect migration failed: %w", err)
	}

	// Handle identify_as_bot field migration (bot mode +B self-announce)
	if err := migrateIdentifyAsBot(db); err != nil {
		return fmt.Errorf("identify_as_bot migration failed: %w", err)
	}

	// Handle channel is_open field migration
	if err := migrateChannelIsOpen(db); err != nil {
		return fmt.Errorf("channel is_open migration failed: %w", err)
	}

	// Handle private message conversations table migration
	if err := migratePrivateMessageConversations(db); err != nil {
		return fmt.Errorf("private message conversations migration failed: %w", err)
	}

	// Handle plugin configs table migration
	if err := migratePluginConfigs(db); err != nil {
		return fmt.Errorf("plugin configs migration failed: %w", err)
	}

	// Handle plugin config column migration
	if err := migratePluginConfigColumn(db); err != nil {
		return fmt.Errorf("plugin config column migration failed: %w", err)
	}

	// Handle FTS5 full-text search migration
	if err := migrateFTS5(db); err != nil {
		return fmt.Errorf("FTS5 migration failed: %w", err)
	}

	// Handle pinned messages table migration
	if err := migratePinnedMessages(db); err != nil {
		return fmt.Errorf("pinned messages migration failed: %w", err)
	}

	// Handle pm_target column migration (adds column + best-effort backfill)
	if err := migratePMTarget(db); err != nil {
		return fmt.Errorf("pm_target migration failed: %w", err)
	}

	// Handle msgid column migration (adds column + partial unique index for CHATHISTORY dedup)
	if err := migrateMsgID(db); err != nil {
		return fmt.Errorf("msgid migration failed: %w", err)
	}

	// Handle IRCv3 reply/context column migration (adds reply_msgid + channel_context)
	if err := migrateReplyAndContext(db); err != nil {
		return fmt.Errorf("reply/context migration failed: %w", err)
	}

	// Normalize legacy mixed-timezone message timestamps to a single UTC text format
	// so the lexicographic scrollback cursor (GetMessagesBeforeTime) works.
	if err := migrateNormalizeMessageTimestamps(db); err != nil {
		return fmt.Errorf("timestamp normalization migration failed: %w", err)
	}

	// Handle settings table migration (durable key/value prefs: theme, accent, ...)
	if err := migrateSettings(db); err != nil {
		return fmt.Errorf("settings migration failed: %w", err)
	}

	// Handle STS policies table migration (durable per-host TLS enforcement)
	if err := migrateSTSPolicies(db); err != nil {
		return fmt.Errorf("STS policies migration failed: %w", err)
	}

	// Handle monitored nicks table migration (durable per-network MONITOR list)
	if err := migrateMonitoredNicks(db); err != nil {
		return fmt.Errorf("monitored nicks migration failed: %w", err)
	}

	// Handle script state table migration (durable enabled/disabled flag per script)
	if err := migrateScriptState(db); err != nil {
		return fmt.Errorf("script state migration failed: %w", err)
	}

	// Handle link previews table migration (persistent cache for click-to-load URL unfurls)
	if err := migrateLinkPreviews(db); err != nil {
		return fmt.Errorf("link previews migration failed: %w", err)
	}

	return nil
}

// migrateNormalizeMessageTimestamps rewrites legacy message timestamps into a single
// canonical UTC text format. Early builds stored live (server-time) messages in UTC
// but join/quit/echo rows in local wall-clock, leaving messages.timestamp with mixed
// offsets ("...+00:00" / "...-07:00" / "...-08:00"). SQLite has no native datetime
// type and compares the column as text, so mixed offsets break `ORDER BY timestamp`
// and the `WHERE timestamp < ?` scrollback cursor (GetMessagesBeforeTime), stalling
// scroll-to-top with older messages unreachable. The write path now forces UTC
// (normalizeForStore); this backfills existing rows.
//
// It is idempotent and cheap on a healthy database: rows already ending in "+00:00"
// are skipped, so once converted the gating count is zero and it returns immediately.
// strftime translates any offset to UTC; rows it can't parse are left untouched
// rather than nulled (the column is NOT NULL), so a stray legacy format degrades to
// "not improved" instead of failing startup.
func migrateNormalizeMessageTimestamps(db *sqlx.DB) error {
	var pending int
	err := db.Get(&pending, "SELECT COUNT(*) FROM messages WHERE timestamp NOT LIKE '%+00:00'")
	if err != nil {
		// A brand-new database may not have the messages table yet; nothing to do.
		if strings.Contains(err.Error(), "no such table") {
			return nil
		}
		return fmt.Errorf("failed to count non-UTC timestamps: %w", err)
	}
	if pending == 0 {
		return nil
	}

	// Single statement = single implicit transaction. strftime converts the stored
	// offset to UTC; the IS NOT NULL guard skips any row it cannot parse.
	if _, err := db.Exec(`
		UPDATE messages
		SET timestamp = strftime('%Y-%m-%d %H:%M:%f', timestamp) || '+00:00'
		WHERE timestamp NOT LIKE '%+00:00'
		  AND strftime('%Y-%m-%d %H:%M:%f', timestamp) IS NOT NULL`); err != nil {
		return fmt.Errorf("failed to normalize message timestamps to UTC: %w", err)
	}

	return nil
}

// migrateMsgID adds the nullable msgid column to the messages table (if missing)
// and creates the partial unique index used to deduplicate CHATHISTORY replays by
// IRCv3 message id. Legacy rows (msgid IS NULL) are exempt from the index, so they
// never collide; only rows that carry a real msgid are deduplicated.
func migrateMsgID(db *sqlx.DB) error {
	var columnExists int
	err := db.Get(&columnExists,
		"SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name='msgid'")
	if err != nil {
		return fmt.Errorf("failed to check for msgid column: %w", err)
	}

	if columnExists == 0 {
		if _, err := db.Exec("ALTER TABLE messages ADD COLUMN msgid TEXT"); err != nil {
			// Ignore "duplicate column" errors
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add msgid column: %w", err)
			}
		}
	}

	// Create the partial unique index (idempotent via IF NOT EXISTS).
	if _, err := db.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_network_msgid ON messages(network_id, msgid) WHERE msgid IS NOT NULL"); err != nil {
		return fmt.Errorf("failed to create msgid unique index: %w", err)
	}

	return nil
}

// migrateReplyAndContext adds the nullable reply_msgid and channel_context
// columns (IRCv3 +draft/reply and +draft/channel-context client tags) to an
// existing messages table. New databases get them from schema.sql; this only
// patches DBs created before the columns existed.
func migrateReplyAndContext(db *sqlx.DB) error {
	for _, col := range []string{"reply_msgid", "channel_context"} {
		var count int
		if err := db.Get(&count,
			"SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name=?", col); err != nil {
			return fmt.Errorf("failed to check for %s column: %w", col, err)
		}
		if count == 0 {
			if _, err := db.Exec("ALTER TABLE messages ADD COLUMN " + col + " TEXT"); err != nil {
				// Tolerate a concurrent/duplicate add.
				if !strings.Contains(err.Error(), "duplicate column name") {
					return fmt.Errorf("failed to add %s column: %w", col, err)
				}
			}
		}
	}
	return nil
}

// migrateRefactorTables handles the refactoring from Server/ServerAddress to Network/Server
func migrateRefactorTables(db *sqlx.DB) error {
	// Check if old 'servers' table exists (before refactoring)
	var oldServersExists int
	err := db.Get(&oldServersExists,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='servers'")
	if err != nil {
		return fmt.Errorf("failed to check for old servers table: %w", err)
	}

	// If old tables exist, we need to rename them
	if oldServersExists > 0 {
		// Check if refactoring has already been done
		var networksExists int
		err = db.Get(&networksExists,
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='networks'")
		if err != nil {
			return fmt.Errorf("failed to check for networks table: %w", err)
		}

		// Only run refactoring if networks table doesn't exist yet
		if networksExists == 0 {
			refactoringMigrations := []string{
				refactorRenameServersToNetworks,
				refactorRenameServerAddressesToServers,
				refactorUpdateForeignKeys,
				refactorUpdateIndexes,
			}

			for i, migration := range refactoringMigrations {
				if _, err := db.Exec(migration); err != nil {
					return fmt.Errorf("refactoring migration %d failed: %w", i+1, err)
				}
			}
		}
	}

	return nil
}

// Refactoring migrations (run once to rename tables)
const refactorRenameServersToNetworks = `
ALTER TABLE servers RENAME TO networks;
`

const refactorRenameServerAddressesToServers = `
ALTER TABLE server_addresses RENAME TO servers;
ALTER TABLE servers RENAME COLUMN server_id TO network_id;
`

const refactorUpdateForeignKeys = `
ALTER TABLE channels RENAME COLUMN server_id TO network_id;
ALTER TABLE messages RENAME COLUMN server_id TO network_id;
`

const refactorUpdateIndexes = `
DROP INDEX IF EXISTS idx_messages_server_channel_time;
DROP INDEX IF EXISTS idx_server_addresses_server_order;
CREATE INDEX IF NOT EXISTS idx_messages_network_channel_time ON messages(network_id, channel_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_servers_network_order ON servers(network_id, "order");
`

// New table creation (after refactoring)
const createNetworksTable = `
CREATE TABLE IF NOT EXISTS networks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    port INTEGER NOT NULL,
    tls BOOLEAN NOT NULL DEFAULT 0,
    nickname TEXT NOT NULL,
    username TEXT NOT NULL,
    realname TEXT NOT NULL,
    password TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const createServersTable = `
CREATE TABLE IF NOT EXISTS servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    address TEXT NOT NULL,
    port INTEGER NOT NULL,
    tls BOOLEAN NOT NULL DEFAULT 0,
    "order" INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE
);
`

const createChannelsTable = `
CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    topic TEXT DEFAULT '',
    modes TEXT DEFAULT '',
    auto_join BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE(network_id, name)
);
`

const createChannelUsersTable = `
CREATE TABLE IF NOT EXISTS channel_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL,
    nickname TEXT NOT NULL,
    modes TEXT DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
    UNIQUE(channel_id, nickname)
);
`

const createMessagesTable = `
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    channel_id INTEGER,
    user TEXT NOT NULL,
    message TEXT NOT NULL,
    message_type TEXT NOT NULL DEFAULT 'privmsg',
    timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    raw_line TEXT,
    pm_target TEXT,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);
`

const createIndexes = `
CREATE INDEX IF NOT EXISTS idx_messages_network_channel_time ON messages(network_id, channel_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_servers_network_order ON servers(network_id, "order");
`

// migrateSASLFields adds SASL fields to networks table if they don't exist
func migrateSASLFields(db *sqlx.DB) error {
	// Check if sasl_enabled column already exists
	var columnExists int
	err := db.Get(&columnExists,
		"SELECT COUNT(*) FROM pragma_table_info('networks') WHERE name='sasl_enabled'")
	if err != nil {
		return fmt.Errorf("failed to check for sasl_enabled column: %w", err)
	}

	// If column doesn't exist, add all SASL columns
	if columnExists == 0 {
		saslMigrations := []string{
			"ALTER TABLE networks ADD COLUMN sasl_enabled BOOLEAN NOT NULL DEFAULT 0",
			"ALTER TABLE networks ADD COLUMN sasl_mechanism TEXT",
			"ALTER TABLE networks ADD COLUMN sasl_username TEXT",
			"ALTER TABLE networks ADD COLUMN sasl_password TEXT",
			"ALTER TABLE networks ADD COLUMN sasl_external_cert TEXT",
		}

		for i, migration := range saslMigrations {
			if _, err := db.Exec(migration); err != nil {
				return fmt.Errorf("SASL migration %d failed: %w", i+1, err)
			}
		}
	}

	return nil
}

// migrateChannelInfo adds topic, modes, and updated_at fields to channels table if they don't exist
func migrateChannelInfo(db *sqlx.DB) error {
	// Check each column individually and add if missing
	columnsToAdd := map[string]string{
		"topic":      "ALTER TABLE channels ADD COLUMN topic TEXT DEFAULT ''",
		"modes":      "ALTER TABLE channels ADD COLUMN modes TEXT DEFAULT ''",
		"updated_at": "ALTER TABLE channels ADD COLUMN updated_at TIMESTAMP",
	}

	for columnName, alterSQL := range columnsToAdd {
		var columnExists int
		err := db.Get(&columnExists,
			"SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name=?", columnName)
		if err != nil {
			return fmt.Errorf("failed to check for %s column: %w", columnName, err)
		}

		if columnExists == 0 {
			if _, err := db.Exec(alterSQL); err != nil {
				// Ignore "duplicate column" errors in case of race conditions
				if !strings.Contains(err.Error(), "duplicate column") {
					return fmt.Errorf("failed to add %s column: %w", columnName, err)
				}
			}
			// For updated_at, set default value for existing rows
			if columnName == "updated_at" {
				if _, err := db.Exec("UPDATE channels SET updated_at = CURRENT_TIMESTAMP WHERE updated_at IS NULL"); err != nil {
					return fmt.Errorf("failed to set default updated_at values: %w", err)
				}
			}
		}
	}

	// Create channel_users table if it doesn't exist
	_, err := db.Exec(createChannelUsersTable)
	if err != nil {
		// Ignore "table already exists" errors
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create channel_users table: %w", err)
		}
	}

	return nil
}

// migrateAutoConnect adds auto_connect field to networks table if it doesn't exist
func migrateAutoConnect(db *sqlx.DB) error {
	var columnExists int
	err := db.Get(&columnExists,
		"SELECT COUNT(*) FROM pragma_table_info('networks') WHERE name='auto_connect'")
	if err != nil {
		return fmt.Errorf("failed to check for auto_connect column: %w", err)
	}

	if columnExists == 0 {
		if _, err := db.Exec("ALTER TABLE networks ADD COLUMN auto_connect BOOLEAN NOT NULL DEFAULT 0"); err != nil {
			// Ignore "duplicate column" errors
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add auto_connect column: %w", err)
			}
		}
	}

	return nil
}

// migrateIdentifyAsBot adds identify_as_bot field to networks table if it doesn't exist
func migrateIdentifyAsBot(db *sqlx.DB) error {
	var columnExists int
	err := db.Get(&columnExists,
		"SELECT COUNT(*) FROM pragma_table_info('networks') WHERE name='identify_as_bot'")
	if err != nil {
		return fmt.Errorf("failed to check for identify_as_bot column: %w", err)
	}

	if columnExists == 0 {
		if _, err := db.Exec("ALTER TABLE networks ADD COLUMN identify_as_bot BOOLEAN NOT NULL DEFAULT 0"); err != nil {
			// Ignore "duplicate column" errors
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add identify_as_bot column: %w", err)
			}
		}
	}

	return nil
}

// migrateChannelIsOpen adds is_open field to channels table if it doesn't exist
func migrateChannelIsOpen(db *sqlx.DB) error {
	var columnExists int
	err := db.Get(&columnExists,
		"SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name='is_open'")
	if err != nil {
		return fmt.Errorf("failed to check for is_open column: %w", err)
	}

	if columnExists == 0 {
		if _, err := db.Exec("ALTER TABLE channels ADD COLUMN is_open BOOLEAN NOT NULL DEFAULT 0"); err != nil {
			// Ignore "duplicate column" errors
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add is_open column: %w", err)
			}
		}
		// Set is_open=true for channels where user is currently a member (they should be open)
		// This is a best-effort migration - we'll update based on actual join status
	}

	return nil
}

// migratePMTarget adds the pm_target column to the messages table (if missing)
// and best-effort backfills it for existing private-message rows.
//
// pm_target holds the conversation peer (the other party) for a PM row, and is
// NULL for channel/status/server rows. For received messages the peer is the
// sender; for messages we sent (sender == our nick, e.g. echo-message) the peer
// is the PRIVMSG target parsed from raw_line. Rows whose target can't be
// determined (sent rows with a malformed/empty raw_line) are left NULL and fall
// back to the legacy raw_line matcher in the frontend.
func migratePMTarget(db *sqlx.DB) error {
	var columnExists int
	err := db.Get(&columnExists,
		"SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name='pm_target'")
	if err != nil {
		return fmt.Errorf("failed to check for pm_target column: %w", err)
	}

	if columnExists == 0 {
		if _, err := db.Exec("ALTER TABLE messages ADD COLUMN pm_target TEXT"); err != nil {
			// Ignore "duplicate column" errors (concurrent/repeat migration)
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add pm_target column: %w", err)
			}
		}
	}

	// Index creation is idempotent and must run for both fresh schemas (where
	// the column is created by createMessagesTable) and migrated databases.
	if _, err := db.Exec(
		"CREATE INDEX IF NOT EXISTS idx_messages_network_pm_target ON messages(network_id, pm_target)"); err != nil {
		return fmt.Errorf("failed to create pm_target index: %w", err)
	}

	// Best-effort backfill of any private-message rows still missing pm_target.
	// This is intentionally NOT gated on whether we just added the column: a
	// crash (or the fts5-less binding generator) can add the column via
	// auto-committed DDL while the backfill rolls back, so we must retry the
	// NULL rows on every startup. A healthy database returns zero rows here.
	type pmRow struct {
		ID       int64          `db:"id"`
		User     string         `db:"user"`
		RawLine  sql.NullString `db:"raw_line"`
		Nickname string         `db:"nickname"`
	}
	var rows []pmRow
	err = db.Select(&rows, `
		SELECT m.id AS id, m.user AS user, m.raw_line AS raw_line, n.nickname AS nickname
		FROM messages m
		JOIN networks n ON n.id = m.network_id
		WHERE m.channel_id IS NULL
		  AND m.message_type IN ('privmsg', 'action')
		  AND m.pm_target IS NULL`)
	if err != nil {
		return fmt.Errorf("failed to load rows for pm_target backfill: %w", err)
	}

	if len(rows) == 0 {
		return nil
	}

	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to begin pm_target backfill tx: %w", err)
	}
	defer tx.Rollback()

	for _, r := range rows {
		var peer string
		if strings.EqualFold(r.User, r.Nickname) {
			// We sent this message; the peer is the PRIVMSG target.
			peer = parsePrivmsgTarget(r.RawLine.String)
		} else {
			// We received this message; the peer is the sender.
			peer = r.User
		}
		if peer == "" {
			// Can't determine the peer; leave NULL for the legacy fallback.
			continue
		}
		if _, err := tx.Exec("UPDATE messages SET pm_target = ? WHERE id = ?", peer, r.ID); err != nil {
			return fmt.Errorf("failed to backfill pm_target for message %d: %w", r.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit pm_target backfill: %w", err)
	}

	return nil
}

// parsePrivmsgTarget extracts the target (first parameter) of a PRIVMSG from a
// raw IRC line, tolerating optional message tags and a source prefix. Returns
// "" if no PRIVMSG target can be found.
func parsePrivmsgTarget(rawLine string) string {
	fields := strings.Fields(rawLine)
	for i, f := range fields {
		if strings.EqualFold(f, "PRIVMSG") && i+1 < len(fields) {
			return strings.TrimPrefix(fields[i+1], ":")
		}
	}
	return ""
}

const createPrivateMessageConversationsTable = `
CREATE TABLE IF NOT EXISTS private_message_conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    target_user TEXT NOT NULL,
    is_open BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE(network_id, target_user)
);
`

// migratePrivateMessageConversations creates the private_message_conversations table and populates it from existing messages
func migratePrivateMessageConversations(db *sqlx.DB) error {
	// Check if table exists
	var tableExists int
	err := db.Get(&tableExists,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='private_message_conversations'")
	if err != nil {
		return fmt.Errorf("failed to check for private_message_conversations table: %w", err)
	}

	if tableExists == 0 {
		// Create the table
		if _, err := db.Exec(createPrivateMessageConversationsTable); err != nil {
			return fmt.Errorf("failed to create private_message_conversations table: %w", err)
		}

		// Populate from existing PM messages
		// Extract unique user pairs from messages where channel_id IS NULL
		// For each network, find all unique users (excluding the network's own nickname)
		// We'll use a subquery to get the network nickname and exclude it
		_, err = db.Exec(`
			INSERT INTO private_message_conversations (network_id, target_user, is_open, created_at)
			SELECT DISTINCT 
				m.network_id,
				LOWER(m.user) as target_user,
				0 as is_open,
				MIN(m.timestamp) as created_at
			FROM messages m
			INNER JOIN networks n ON m.network_id = n.id
			WHERE m.channel_id IS NULL 
				AND m.message_type IN ('privmsg', 'action')
				AND m.user != '*'
				AND LOWER(m.user) != LOWER(n.nickname)
			GROUP BY m.network_id, LOWER(m.user)
		`)
		if err != nil {
			// Ignore errors if there are no PM messages yet
			if !strings.Contains(err.Error(), "no such table") && !strings.Contains(err.Error(), "no rows") {
				return fmt.Errorf("failed to populate private_message_conversations: %w", err)
			}
		}
	}

	return nil
}

const createPluginConfigsTable = `
CREATE TABLE IF NOT EXISTS plugin_configs (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    config TEXT,
    config_schema TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// migratePluginConfigs creates the plugin_configs table if it doesn't exist
func migratePluginConfigs(db *sqlx.DB) error {
	// Check if table exists
	var tableExists int
	err := db.Get(&tableExists,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='plugin_configs'")
	if err != nil {
		return fmt.Errorf("failed to check for plugin_configs table: %w", err)
	}

	if tableExists == 0 {
		// Create the table
		if _, err := db.Exec(createPluginConfigsTable); err != nil {
			return fmt.Errorf("failed to create plugin_configs table: %w", err)
		}
	}

	return nil
}

// migratePluginConfigColumn adds the config and config_schema JSON columns to plugin_configs table if they don't exist
func migratePluginConfigColumn(db *sqlx.DB) error {
	columnsToAdd := map[string]string{
		"config":        "ALTER TABLE plugin_configs ADD COLUMN config TEXT",
		"config_schema": "ALTER TABLE plugin_configs ADD COLUMN config_schema TEXT",
	}

	for columnName, alterSQL := range columnsToAdd {
		var columnExists int
		err := db.Get(&columnExists,
			"SELECT COUNT(*) FROM pragma_table_info('plugin_configs') WHERE name=?", columnName)
		if err != nil {
			return fmt.Errorf("failed to check for %s column: %w", columnName, err)
		}

		if columnExists == 0 {
			if _, err := db.Exec(alterSQL); err != nil {
				// Ignore "duplicate column" errors
				if !strings.Contains(err.Error(), "duplicate column") {
					return fmt.Errorf("failed to add %s column: %w", columnName, err)
				}
			}
		}
	}

	return nil
}

const createPinnedMessagesTable = `
CREATE TABLE IF NOT EXISTS pinned_messages (
    message_id INTEGER PRIMARY KEY,
    network_id INTEGER NOT NULL,
    channel_id INTEGER,
    pinned_by  TEXT NOT NULL DEFAULT '',
    pinned_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);
`

// migratePinnedMessages creates the pinned_messages table and its index if they don't exist
func migratePinnedMessages(db *sqlx.DB) error {
	var tableExists int
	err := db.Get(&tableExists,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pinned_messages'")
	if err != nil {
		return fmt.Errorf("failed to check for pinned_messages table: %w", err)
	}

	if tableExists == 0 {
		if _, err := db.Exec(createPinnedMessagesTable); err != nil {
			return fmt.Errorf("failed to create pinned_messages table: %w", err)
		}
	}

	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_pinned_network_channel ON pinned_messages(network_id, channel_id)"); err != nil {
		return fmt.Errorf("failed to create pinned_messages index: %w", err)
	}

	return nil
}

// migrateFTS5 creates the FTS5 virtual table and triggers for full-text message search
func migrateFTS5(db *sqlx.DB) error {
	// Check if FTS5 table already exists
	var tableExists int
	err := db.Get(&tableExists,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages_fts'")
	if err != nil {
		return fmt.Errorf("failed to check for messages_fts table: %w", err)
	}

	if tableExists == 0 {
		// Create the FTS5 virtual table
		_, err = db.Exec(`
			CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
				message,
				user,
				content='messages',
				content_rowid='id'
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create messages_fts table: %w", err)
		}

		// Populate FTS5 table from existing messages
		_, err = db.Exec(`
			INSERT INTO messages_fts(rowid, message, user)
			SELECT id, message, user FROM messages
		`)
		if err != nil {
			// Non-fatal: if there are no messages yet, this is fine
			if !strings.Contains(err.Error(), "no such table") {
				return fmt.Errorf("failed to populate messages_fts: %w", err)
			}
		}
	}

	// Create triggers (IF NOT EXISTS handles idempotency)
	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, message, user) VALUES (new.id, new.message, new.user);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, message, user) VALUES('delete', old.id, old.message, old.user);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, message, user) VALUES('delete', old.id, old.message, old.user);
			INSERT INTO messages_fts(rowid, message, user) VALUES (new.id, new.message, new.user);
		END`,
	}

	for i, trigger := range triggers {
		if _, err := db.Exec(trigger); err != nil {
			return fmt.Errorf("FTS5 trigger %d failed: %w", i+1, err)
		}
	}

	return nil
}

const createSettingsTable = `
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// migrateSettings creates the settings key/value table if it doesn't exist.
// This is the durable home for UI preferences (theme mode, accent) that the
// frontend can no longer persist in the WKWebView's localStorage.
func migrateSettings(db *sqlx.DB) error {
	if _, err := db.Exec(createSettingsTable); err != nil {
		return fmt.Errorf("failed to create settings table: %w", err)
	}
	return nil
}

const createSTSPoliciesTable = `
CREATE TABLE IF NOT EXISTS sts_policies (
    hostname   TEXT PRIMARY KEY,
    port       INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const createMonitoredNicksTable = `
CREATE TABLE IF NOT EXISTS monitored_nicks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    nickname TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE(network_id, nickname)
);
`

// migrateMonitoredNicks creates the monitored_nicks table if it doesn't exist. It
// is the durable per-network MONITOR buddy list re-sent to the server on connect.
func migrateMonitoredNicks(db *sqlx.DB) error {
	if _, err := db.Exec(createMonitoredNicksTable); err != nil {
		return fmt.Errorf("failed to create monitored_nicks table: %w", err)
	}
	return nil
}

// migrateSTSPolicies creates the sts_policies table if it doesn't exist. It is the
// durable store for IRCv3 STS policies (hostname -> secure port + expiry) that
// force future connections to a host onto TLS.
func migrateSTSPolicies(db *sqlx.DB) error {
	if _, err := db.Exec(createSTSPoliciesTable); err != nil {
		return fmt.Errorf("failed to create sts_policies table: %w", err)
	}
	return nil
}

const createScriptStateTable = `
CREATE TABLE IF NOT EXISTS script_state (
    script_id TEXT PRIMARY KEY,
    enabled   INTEGER NOT NULL DEFAULT 1
);
`

// migrateScriptState creates the script_state table if it doesn't exist.
// It stores the durable enabled/disabled flag for each cascade script; a missing
// row means enabled (the DEFAULT 1 covers fresh rows), so only explicitly-disabled
// scripts need a row.
func migrateScriptState(db *sqlx.DB) error {
	if _, err := db.Exec(createScriptStateTable); err != nil {
		return fmt.Errorf("failed to create script_state table: %w", err)
	}
	return nil
}

const createLinkPreviewsTable = `
CREATE TABLE IF NOT EXISTS link_previews (
    url          TEXT PRIMARY KEY,
    status       TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    site_name    TEXT NOT NULL DEFAULT '',
    image_data   TEXT NOT NULL DEFAULT '',
    fetched_at   INTEGER NOT NULL
);
`

// migrateLinkPreviews creates the link_previews table if it doesn't exist.
// It is the persistent cache for click-to-load URL preview cards. Rows are
// pruned by age (TTL-aware reads treat stale rows as absent) and by count
// (PruneLinkPreviews keeps only the N most-recently-fetched rows).
func migrateLinkPreviews(db *sqlx.DB) error {
	if _, err := db.Exec(createLinkPreviewsTable); err != nil {
		return fmt.Errorf("failed to create link_previews table: %w", err)
	}
	return nil
}
