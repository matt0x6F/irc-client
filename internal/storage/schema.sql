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
    identify_as_bot BOOLEAN NOT NULL DEFAULT 0,
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
    pm_target TEXT, -- conversation peer for private messages (NULL for channel/status/server rows)
    msgid TEXT, -- IRCv3 message id (NULL for legacy/local rows); used to dedup CHATHISTORY replays
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

-- Durable key/value store for application/UI preferences (theme mode, accent,
-- etc.). The frontend previously kept these in the WKWebView's localStorage,
-- which macOS does not persist across app restarts; storing them here keeps them
-- alongside the rest of the app's state.
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Cache of click-to-load link previews. Keyed by the requested URL; image_data
-- holds a self-contained data URI (never a remote src). status is "ok" or
-- "blocked". fetched_at is unix seconds so reads can apply a TTL with a plain
-- numeric comparison. Rows are pruned by age (TTL) and by count (size cap).
CREATE TABLE IF NOT EXISTS link_previews (
    url          TEXT PRIMARY KEY,
    status       TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    site_name    TEXT NOT NULL DEFAULT '',
    image_data   TEXT NOT NULL DEFAULT '',
    fetched_at   INTEGER NOT NULL
);

-- IRCv3 STS (Strict Transport Security) policies. Keyed by hostname (UA-wide, per
-- spec) and independent of any network row, so a policy learned on one network
-- entry protects every connection to that host. expires_at is stored as unix
-- seconds for unambiguous numeric comparison against time.Now().Unix().
CREATE TABLE IF NOT EXISTS sts_policies (
    hostname   TEXT PRIMARY KEY,
    port       INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Durable enabled/disabled state for cascade scripts. A missing row means
-- enabled (default 1); only disabled scripts need a row.
CREATE TABLE IF NOT EXISTS script_state (
    script_id TEXT PRIMARY KEY,
    enabled   INTEGER NOT NULL DEFAULT 1
);

-- IRCv3 MONITOR: the per-network buddy list of nicks whose online/offline
-- presence is tracked. Durable, and re-sent via MONITOR + on each connect.
CREATE TABLE IF NOT EXISTS monitored_nicks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    network_id INTEGER NOT NULL,
    nickname TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (network_id) REFERENCES networks(id) ON DELETE CASCADE,
    UNIQUE(network_id, nickname)
);

CREATE INDEX IF NOT EXISTS idx_messages_network_channel_time ON messages(network_id, channel_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
-- Partial unique index: dedup CHATHISTORY replays by IRCv3 msgid, while leaving
-- legacy/local rows (msgid IS NULL) exempt so they never collide.
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_network_msgid ON messages(network_id, msgid) WHERE msgid IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_servers_network_order ON servers(network_id, "order");
CREATE INDEX IF NOT EXISTS idx_pinned_network_channel ON pinned_messages(network_id, channel_id);

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
