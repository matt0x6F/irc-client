package storage

import (
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

	// Handle channel is_open field migration
	if err := migrateChannelIsOpen(db); err != nil {
		return fmt.Errorf("channel is_open migration failed: %w", err)
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
		"topic":     "ALTER TABLE channels ADD COLUMN topic TEXT DEFAULT ''",
		"modes":     "ALTER TABLE channels ADD COLUMN modes TEXT DEFAULT ''",
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

