package storage

import "time"

// Network represents an IRC network configuration
type Network struct {
	ID       int64  `db:"id" json:"id"`
	Name     string `db:"name" json:"name"`
	Address  string `db:"address" json:"address"` // Deprecated: use Servers
	Port     int    `db:"port" json:"port"`       // Deprecated: use Servers
	TLS      bool   `db:"tls" json:"tls"`         // Deprecated: use Servers
	Nickname string `db:"nickname" json:"nickname"`
	Username string `db:"username" json:"username"`
	Realname string `db:"realname" json:"realname"`
	// Password and SASLPassword live in the OS keychain, not the database. These
	// columns hold values only as a fallback when the keychain is unavailable, or
	// transiently for legacy rows awaiting lazy migration; they are never
	// serialized to the frontend (json:"-") — see the Has* flags below.
	// SASLExternalCert is a client-certificate *path*, not a secret, so it stays a
	// normal field.
	Password         string    `db:"password" json:"-"`
	SASLEnabled      bool      `db:"sasl_enabled" json:"sasl_enabled"`
	SASLMechanism    *string   `db:"sasl_mechanism" json:"sasl_mechanism"`
	SASLUsername     *string   `db:"sasl_username" json:"sasl_username"`
	SASLPassword     *string   `db:"sasl_password" json:"-"`
	SASLExternalCert *string   `db:"sasl_external_cert" json:"sasl_external_cert"`
	AutoConnect      bool      `db:"auto_connect" json:"auto_connect"`
	IdentifyAsBot    bool      `db:"identify_as_bot" json:"identify_as_bot"`
	CreatedAt        time.Time `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time `db:"updated_at" json:"updated_at"`

	// Computed, non-persisted flags populated by the App layer for the frontend.
	// Has* report whether a secret is set (keychain or fallback column) without
	// exposing the value; CredentialStorageInsecure is true when any secret is
	// currently held in a plaintext column rather than the keychain.
	HasPassword               bool `db:"-" json:"hasPassword"`
	HasSASLPassword           bool `db:"-" json:"hasSaslPassword"`
	CredentialStorageInsecure bool `db:"-" json:"credentialStorageInsecure"`
}

// Server represents a single server address within a network
type Server struct {
	ID        int64     `db:"id" json:"id"`
	NetworkID int64     `db:"network_id" json:"network_id"`
	Address   string    `db:"address" json:"address"`
	Port      int       `db:"port" json:"port"`
	TLS       bool      `db:"tls" json:"tls"`
	Order     int       `db:"order" json:"order"` // Order for fallback (lower = higher priority)
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// STSPolicy is a persisted IRCv3 STS (Strict Transport Security) policy: a host
// the client has learned (over TLS) must always be reached via TLS on Port until
// ExpiresAt. Keyed by hostname, UA-wide. ExpiresAt is unix seconds.
type STSPolicy struct {
	Hostname  string `db:"hostname" json:"hostname"`
	Port      int    `db:"port" json:"port"`
	ExpiresAt int64  `db:"expires_at" json:"expires_at"`
}

// Channel represents an IRC channel
// State: OPEN (dialog open, may or may not be joined), JOINED (dialog open and joined), CLOSED (dialog closed, not joined)
type Channel struct {
	ID        int64      `db:"id" json:"id"`
	NetworkID int64      `db:"network_id" json:"network_id"`
	Name      string     `db:"name" json:"name"`
	Topic     string     `db:"topic" json:"topic"`
	Modes     string     `db:"modes" json:"modes"`
	AutoJoin  bool       `db:"auto_join" json:"auto_join"`
	IsOpen    bool       `db:"is_open" json:"is_open"` // Dialog/pane is open (OPEN or JOINED state)
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt *time.Time `db:"updated_at" json:"updated_at"`
}

// ChannelUser represents a user in a channel
type ChannelUser struct {
	ID        int64     `db:"id" json:"id"`
	ChannelID int64     `db:"channel_id" json:"channel_id"`
	Nickname  string    `db:"nickname" json:"nickname"`
	Modes     string    `db:"modes" json:"modes"` // e.g., "@" for op, "+" for voice
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// Message represents an IRC message
type Message struct {
	ID             int64     `db:"id" json:"id"`
	NetworkID      int64     `db:"network_id" json:"network_id"`
	ChannelID      *int64    `db:"channel_id" json:"channel_id"` // Nullable for private messages
	User           string    `db:"user" json:"user"`
	Message        string    `db:"message" json:"message"`
	MessageType    string    `db:"message_type" json:"message_type"` // 'privmsg', 'notice', 'action', etc.
	Timestamp      time.Time `db:"timestamp" json:"timestamp"`
	RawLine        string    `db:"raw_line" json:"raw_line"`               // Original IRC line
	PMTarget       string    `db:"pm_target" json:"pm_target"`             // Conversation peer for PMs ("" for channel/status/server rows)
	MsgID          string    `db:"msgid" json:"msgid"`                     // IRCv3 message id ("" for legacy/local rows); dedup key for CHATHISTORY
	ReplyMsgID     string    `db:"reply_msgid" json:"reply_msgid"`         // IRCv3 +draft/reply: msgid of the parent message ("" if not a reply)
	ChannelContext string    `db:"channel_context" json:"channel_context"` // IRCv3 +draft/channel-context: channel a PM is about ("" otherwise)
}

// PinnedMessage represents a message that has been pinned, with pin metadata
type PinnedMessage struct {
	Message
	PinnedBy string    `db:"pinned_by" json:"pinned_by"`
	PinnedAt time.Time `db:"pinned_at" json:"pinned_at"`
}

// PrivateMessageConversation represents a private message conversation with a user
type PrivateMessageConversation struct {
	ID         int64      `db:"id" json:"id"`
	NetworkID  int64      `db:"network_id" json:"network_id"`
	TargetUser string     `db:"target_user" json:"target_user"` // The other user in the conversation (lowercase for case-insensitive matching)
	IsOpen     bool       `db:"is_open" json:"is_open"`         // Dialog/pane is open
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt  *time.Time `db:"updated_at" json:"updated_at"`
}

// CachedPreview is a stored link-preview row. FetchedAt is unix seconds.
type CachedPreview struct {
	URL          string
	Status       string
	Title        string
	Description  string
	SiteName     string
	ImageDataURI string
	FetchedAt    int64
}

// PluginConfig represents user configuration for a plugin
type PluginConfig struct {
	Name         string                 `db:"name" json:"name"`
	Enabled      bool                   `db:"enabled" json:"enabled"`
	Config       map[string]interface{} `db:"config" json:"config,omitempty"`               // JSON stored as TEXT
	ConfigSchema map[string]interface{} `db:"config_schema" json:"config_schema,omitempty"` // JSON stored as TEXT
	CreatedAt    time.Time              `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time              `db:"updated_at" json:"updated_at"`
}
