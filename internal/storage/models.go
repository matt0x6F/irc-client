package storage

import "time"

// Network represents an IRC network configuration
type Network struct {
	ID              int64     `db:"id" json:"id"`
	Name            string    `db:"name" json:"name"`
	Address         string    `db:"address" json:"address"` // Deprecated: use Servers
	Port            int       `db:"port" json:"port"`       // Deprecated: use Servers
	TLS             bool      `db:"tls" json:"tls"`         // Deprecated: use Servers
	Nickname        string    `db:"nickname" json:"nickname"`
	Username        string    `db:"username" json:"username"`
	Realname        string    `db:"realname" json:"realname"`
	Password        string    `db:"password" json:"password"` // Should be encrypted
	SASLEnabled     bool      `db:"sasl_enabled" json:"sasl_enabled"`
	SASLMechanism   *string   `db:"sasl_mechanism" json:"sasl_mechanism"`
	SASLUsername    *string   `db:"sasl_username" json:"sasl_username"`
	SASLPassword    *string   `db:"sasl_password" json:"sasl_password"`
	SASLExternalCert *string  `db:"sasl_external_cert" json:"sasl_external_cert"`
	AutoConnect     bool      `db:"auto_connect" json:"auto_connect"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time `db:"updated_at" json:"updated_at"`
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
	ID          int64     `db:"id" json:"id"`
	NetworkID   int64     `db:"network_id" json:"network_id"`
	ChannelID   *int64    `db:"channel_id" json:"channel_id"` // Nullable for private messages
	User        string    `db:"user" json:"user"`
	Message     string    `db:"message" json:"message"`
	MessageType string    `db:"message_type" json:"message_type"` // 'privmsg', 'notice', 'action', etc.
	Timestamp   time.Time `db:"timestamp" json:"timestamp"`
	RawLine     string    `db:"raw_line" json:"raw_line"` // Original IRC line
}

