-- Schema definition for SQLC code generation
-- This should be kept in sync with migrations.go

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
    sasl_enabled BOOLEAN NOT NULL DEFAULT 0,
    sasl_mechanism TEXT,
    sasl_username TEXT,
    sasl_password TEXT,
    sasl_external_cert TEXT,
    auto_connect BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

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

CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    topic TEXT DEFAULT '',
    modes TEXT DEFAULT '',
    auto_join BOOLEAN NOT NULL DEFAULT 0,
    is_open BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE(network_id, name)
);

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

CREATE TABLE IF NOT EXISTS plugin_configs (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    config TEXT,
    config_schema TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_network_channel_time ON messages(network_id, channel_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_servers_network_order ON servers(network_id, "order");

-- FTS5 full-text search index for messages
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    message,
    user,
    content='messages',
    content_rowid='id'
);

-- Triggers to keep FTS5 index in sync with messages table
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, message, user) VALUES (new.id, new.message, new.user);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, message, user) VALUES('delete', old.id, old.message, old.user);
END;

CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, message, user) VALUES('delete', old.id, old.message, old.user);
    INSERT INTO messages_fts(rowid, message, user) VALUES (new.id, new.message, new.user);
END;
