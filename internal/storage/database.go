package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/matt0x6f/irc-client/internal/logger"
	_ "github.com/mattn/go-sqlite3"
)

// Storage handles database operations
type Storage struct {
	db            *sqlx.DB
	writeBuffer   chan Message
	bufferSize    int
	flushInterval time.Duration
	mu            sync.RWMutex
	stopCh        chan struct{}
	wg            sync.WaitGroup
	closed        bool
	closedMu      sync.RWMutex
}

// NewStorage creates a new storage instance
func NewStorage(dbPath string, bufferSize int, flushInterval time.Duration) (*Storage, error) {
	// Enable WAL mode for better concurrent writes
	db, err := sqlx.Connect("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite works best with single connection in WAL mode
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	storage := &Storage{
		db:            db,
		writeBuffer:   make(chan Message, bufferSize),
		bufferSize:    bufferSize,
		flushInterval: flushInterval,
		stopCh:        make(chan struct{}),
	}

	// Run migrations
	if err := Migrate(db); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	// Start background flush goroutine
	storage.wg.Add(1)
	go storage.flushLoop()

	return storage, nil
}

// Close closes the database connection and flushes remaining messages
func (s *Storage) Close() error {
	s.closedMu.Lock()
	s.closed = true
	s.closedMu.Unlock()

	// Close writeBuffer first to prevent new writes
	close(s.writeBuffer)

	// Signal flushLoop to stop
	close(s.stopCh)

	// Wait for flushLoop to finish - it should exit quickly when stopCh is closed
	// The flushLoop will check if storage is closed before doing any database operations
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	// Wait for flushLoop to finish (with a reasonable timeout as safety net)
	select {
	case <-done:
		// flushLoop finished
	case <-time.After(500 * time.Millisecond):
		// If flushLoop is stuck in a database operation, it should have checked
		// if storage is closed and exited. If it's still running after 500ms,
		// something is wrong, but we'll continue anyway.
		logger.Log.Debug().Msg("flushLoop still running after 500ms, proceeding with database close")
	}

	// Try to flush any remaining messages, but don't block if it's slow
	// Use a goroutine with timeout to avoid blocking shutdown
	flushDone := make(chan struct{})
	go func() {
		s.flushBuffer()
		close(flushDone)
	}()

	select {
	case <-flushDone:
		// Flush completed
	case <-time.After(200 * time.Millisecond):
		// Flush is taking too long, skip it
		logger.Log.Debug().Msg("Skipping final flush due to timeout")
	}

	return s.db.Close()
}

// flushLoop periodically flushes the write buffer
func (s *Storage) flushLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			// Storage is closing - flush any remaining messages and exit
			s.flushBuffer()
			return
		case <-ticker.C:
			// Check if storage is closed before flushing
			s.closedMu.RLock()
			closed := s.closed
			s.closedMu.RUnlock()
			if closed {
				// Storage is closed, exit immediately
				return
			}
			s.flushBuffer()
		}
	}
}

// flushBuffer flushes all messages in the buffer to the database
func (s *Storage) flushBuffer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if storage is closed - if so, don't do any database operations
	s.closedMu.RLock()
	closed := s.closed
	s.closedMu.RUnlock()
	if closed {
		return
	}

	if len(s.writeBuffer) == 0 {
		return
	}

	messages := make([]Message, 0, s.bufferSize)
	for {
		select {
		case msg := <-s.writeBuffer:
			messages = append(messages, msg)
		default:
			// No more messages available, break out of loop
			if len(messages) == 0 {
				return
			}
			// Check again if storage is closed before doing database operation
			s.closedMu.RLock()
			closed = s.closed
			s.closedMu.RUnlock()
			if closed {
				return
			}

			// Batch insert
			query := `INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line)
			          VALUES (:network_id, :channel_id, :user, :message, :message_type, :timestamp, :raw_line)`

			_, err := s.db.NamedExec(query, messages)
			if err != nil {
				logger.Log.Error().Err(err).Int("count", len(messages)).Msg("Error flushing messages")
				// Re-queue messages? For now, we'll lose them on error
			}
			return
		}
	}
}

// WriteMessage queues a message for batch insertion
func (s *Storage) WriteMessage(msg Message) error {
	// Check if storage is closed
	s.closedMu.RLock()
	if s.closed {
		s.closedMu.RUnlock()
		return fmt.Errorf("storage is closed")
	}
	s.closedMu.RUnlock()

	// Use recover to handle panic if channel is closed
	defer func() {
		if r := recover(); r != nil {
			// Channel was closed, storage is shutting down
			// Silently ignore - this is expected during shutdown
		}
	}()

	select {
	case s.writeBuffer <- msg:
		return nil
	default:
		// Buffer full, flush immediately
		s.flushBuffer()
		select {
		case s.writeBuffer <- msg:
			return nil
		default:
			return fmt.Errorf("write buffer full and flush failed")
		}
	}
}

// WriteMessageSync writes a message immediately (flushes buffer first)
// Use this for messages that need to be immediately available (e.g., sent messages)
func (s *Storage) WriteMessageSync(msg Message) error {
	// Check if storage is closed
	s.closedMu.RLock()
	if s.closed {
		s.closedMu.RUnlock()
		return fmt.Errorf("storage is closed")
	}
	s.closedMu.RUnlock()

	// Use recover to handle panic if channel is closed
	defer func() {
		if r := recover(); r != nil {
			// Channel was closed, storage is shutting down
			// Silently ignore - this is expected during shutdown
		}
	}()

	// Flush buffer first to ensure we can read it back immediately
	s.flushBuffer()

	// Write the message
	select {
	case s.writeBuffer <- msg:
		// Immediately flush again to ensure it's in the database
		s.flushBuffer()
		return nil
	default:
		// Buffer still full after flush, try direct insert
		query := `INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line)
		          VALUES (:network_id, :channel_id, :user, :message, :message_type, :timestamp, :raw_line)`
		_, err := s.db.NamedExec(query, msg)
		return err
	}
}

// WriteMessageDirect writes a message directly to the database, bypassing the buffer
// Use this during shutdown to ensure messages are persisted
func (s *Storage) WriteMessageDirect(msg Message) error {
	// Check if storage is closed
	s.closedMu.RLock()
	if s.closed {
		s.closedMu.RUnlock()
		return fmt.Errorf("storage is closed")
	}
	s.closedMu.RUnlock()

	// Direct insert to database
	query := `INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line)
	          VALUES (:network_id, :channel_id, :user, :message, :message_type, :timestamp, :raw_line)`
	_, err := s.db.NamedExec(query, msg)
	return err
}

// GetMessages retrieves messages for a network and channel
func (s *Storage) GetMessages(networkID int64, channelID *int64, limit int) ([]Message, error) {
	var messages []Message
	var err error

	if channelID != nil {
		err = s.db.Select(&messages,
			`SELECT * FROM messages 
			 WHERE network_id = ? AND channel_id = ? 
			 ORDER BY timestamp DESC 
			 LIMIT ?`,
			networkID, *channelID, limit)
	} else {
		err = s.db.Select(&messages,
			`SELECT * FROM messages 
			 WHERE network_id = ? AND channel_id IS NULL 
			 ORDER BY timestamp DESC 
			 LIMIT ?`,
			networkID, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// CreateNetwork creates a new network configuration
func (s *Storage) CreateNetwork(network *Network) error {
	query := `INSERT INTO networks (name, address, port, tls, nickname, username, realname, password, sasl_enabled, sasl_mechanism, sasl_username, sasl_password, sasl_external_cert, auto_connect, created_at, updated_at)
	          VALUES (:name, :address, :port, :tls, :nickname, :username, :realname, :password, :sasl_enabled, :sasl_mechanism, :sasl_username, :sasl_password, :sasl_external_cert, :auto_connect, :created_at, :updated_at)`

	result, err := s.db.NamedExec(query, network)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get network ID: %w", err)
	}

	network.ID = id
	return nil
}

// GetNetworks retrieves all networks
func (s *Storage) GetNetworks() ([]Network, error) {
	var networks []Network
	err := s.db.Select(&networks, "SELECT * FROM networks ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("failed to get networks: %w", err)
	}
	return networks, nil
}

// GetNetwork retrieves a network by ID
func (s *Storage) GetNetwork(networkID int64) (*Network, error) {
	var network Network
	err := s.db.Get(&network, "SELECT * FROM networks WHERE id = ?", networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network: %w", err)
	}
	return &network, nil
}

// UpdateNetwork updates a network configuration
func (s *Storage) UpdateNetwork(network *Network) error {
	query := `UPDATE networks 
	          SET name = :name, address = :address, port = :port, tls = :tls, 
	              nickname = :nickname, username = :username, realname = :realname, 
	              password = :password, sasl_enabled = :sasl_enabled, sasl_mechanism = :sasl_mechanism,
	              sasl_username = :sasl_username, sasl_password = :sasl_password, sasl_external_cert = :sasl_external_cert,
	              auto_connect = :auto_connect, updated_at = :updated_at
	          WHERE id = :id`

	_, err := s.db.NamedExec(query, network)
	if err != nil {
		return fmt.Errorf("failed to update network: %w", err)
	}
	return nil
}

// UpdateNetworkAutoConnect updates the auto-connect setting for a network
func (s *Storage) UpdateNetworkAutoConnect(networkID int64, autoConnect bool) error {
	_, err := s.db.Exec("UPDATE networks SET auto_connect = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", autoConnect, networkID)
	return err
}

// DeleteNetwork deletes a network (cascade will delete servers)
func (s *Storage) DeleteNetwork(networkID int64) error {
	_, err := s.db.Exec("DELETE FROM networks WHERE id = ?", networkID)
	return err
}

// GetServers retrieves all server addresses for a network, ordered by priority
func (s *Storage) GetServers(networkID int64) ([]Server, error) {
	var servers []Server
	err := s.db.Select(&servers,
		`SELECT * FROM servers 
		 WHERE network_id = ? 
		 ORDER BY "order" ASC, id ASC`,
		networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get servers: %w", err)
	}
	return servers, nil
}

// CreateServer creates a new server address
func (s *Storage) CreateServer(server *Server) error {
	query := `INSERT INTO servers (network_id, address, port, tls, "order", created_at)
	          VALUES (:network_id, :address, :port, :tls, :order, :created_at)`

	result, err := s.db.NamedExec(query, server)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get server ID: %w", err)
	}

	server.ID = id
	return nil
}

// UpdateServer updates a server address
func (s *Storage) UpdateServer(server *Server) error {
	query := `UPDATE servers 
	          SET address = :address, port = :port, tls = :tls, "order" = :order
	          WHERE id = :id`

	_, err := s.db.NamedExec(query, server)
	if err != nil {
		return fmt.Errorf("failed to update server: %w", err)
	}
	return nil
}

// DeleteServer deletes a server address
func (s *Storage) DeleteServer(serverID int64) error {
	_, err := s.db.Exec("DELETE FROM servers WHERE id = ?", serverID)
	return err
}

// DeleteAllServers deletes all server addresses for a network
func (s *Storage) DeleteAllServers(networkID int64) error {
	_, err := s.db.Exec("DELETE FROM servers WHERE network_id = ?", networkID)
	return err
}

// CreateChannel creates a new channel
func (s *Storage) CreateChannel(channel *Channel) error {
	query := `INSERT INTO channels (network_id, name, auto_join, is_open, created_at)
	          VALUES (:network_id, :name, :auto_join, :is_open, :created_at)`

	result, err := s.db.NamedExec(query, channel)
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get channel ID: %w", err)
	}

	channel.ID = id
	return nil
}

// GetChannels retrieves channels for a network
func (s *Storage) GetChannels(networkID int64) ([]Channel, error) {
	var channels []Channel
	err := s.db.Select(&channels, "SELECT * FROM channels WHERE network_id = ? ORDER BY name", networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channels: %w", err)
	}
	return channels, nil
}

// GetJoinedChannels retrieves channels for a network where the specified nickname is a member
func (s *Storage) GetJoinedChannels(networkID int64, nickname string) ([]Channel, error) {
	query := `
		SELECT DISTINCT c.* 
		FROM channels c
		INNER JOIN channel_users cu ON c.id = cu.channel_id
		WHERE c.network_id = ? AND cu.nickname = ?
		ORDER BY c.name
	`
	var channels []Channel
	err := s.db.Select(&channels, query, networkID, nickname)
	if err != nil {
		return nil, fmt.Errorf("failed to get joined channels: %w", err)
	}
	return channels, nil
}

// GetChannelByName retrieves a channel by network ID and channel name
// Channel names are case-insensitive for IRC channels (channels starting with # or &)
func (s *Storage) GetChannelByName(networkID int64, channelName string) (*Channel, error) {
	var channel Channel
	// IRC channel names are case-insensitive, so use case-insensitive comparison
	// SQLite's default collation is case-insensitive for ASCII, but we'll use LOWER() to be explicit
	err := s.db.Get(&channel, "SELECT * FROM channels WHERE network_id = ? AND LOWER(name) = LOWER(?)", networkID, channelName)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel: %w", err)
	}
	return &channel, nil
}

// UpdateChannelTopic updates the topic for a channel
func (s *Storage) UpdateChannelTopic(channelID int64, topic string) error {
	_, err := s.db.Exec("UPDATE channels SET topic = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", topic, channelID)
	return err
}

// UpdateChannelModes updates the modes for a channel
func (s *Storage) UpdateChannelModes(channelID int64, modes string) error {
	_, err := s.db.Exec("UPDATE channels SET modes = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", modes, channelID)
	return err
}

// UpdateChannelAutoJoin updates the auto-join setting for a channel
func (s *Storage) UpdateChannelAutoJoin(channelID int64, autoJoin bool) error {
	_, err := s.db.Exec("UPDATE channels SET auto_join = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", autoJoin, channelID)
	return err
}

// UpdateChannelIsOpen updates the is_open state for a channel
func (s *Storage) UpdateChannelIsOpen(channelID int64, isOpen bool) error {
	_, err := s.db.Exec("UPDATE channels SET is_open = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", isOpen, channelID)
	return err
}

// GetOpenChannels retrieves channels for a network where the dialog is open (is_open=true) or where the user is joined
func (s *Storage) GetOpenChannels(networkID int64, nickname string) ([]Channel, error) {
	query := `
		SELECT DISTINCT c.* 
		FROM channels c
		LEFT JOIN channel_users cu ON c.id = cu.channel_id AND cu.nickname = ?
		WHERE c.network_id = ? AND (c.is_open = 1 OR cu.nickname IS NOT NULL)
		ORDER BY c.name
	`
	var channels []Channel
	err := s.db.Select(&channels, query, nickname, networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get open channels: %w", err)
	}
	return channels, nil
}

// GetChannelUsers retrieves all users for a channel
func (s *Storage) GetChannelUsers(channelID int64) ([]ChannelUser, error) {
	var users []ChannelUser
	err := s.db.Select(&users, "SELECT * FROM channel_users WHERE channel_id = ? ORDER BY nickname", channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel users: %w", err)
	}
	return users, nil
}

// AddChannelUser adds or updates a user in a channel
func (s *Storage) AddChannelUser(channelID int64, nickname string, modes string) error {
	query := `INSERT INTO channel_users (channel_id, nickname, modes, created_at, updated_at)
	          VALUES (:channel_id, :nickname, :modes, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	          ON CONFLICT(channel_id, nickname) DO UPDATE SET modes = :modes, updated_at = CURRENT_TIMESTAMP`

	user := ChannelUser{
		ChannelID: channelID,
		Nickname:  nickname,
		Modes:     modes,
	}
	_, err := s.db.NamedExec(query, user)
	return err
}

// RemoveChannelUser removes a user from a channel
func (s *Storage) RemoveChannelUser(channelID int64, nickname string) error {
	_, err := s.db.Exec("DELETE FROM channel_users WHERE channel_id = ? AND nickname = ?", channelID, nickname)
	return err
}

// ClearChannelUsers removes all users from a channel
func (s *Storage) ClearChannelUsers(channelID int64) error {
	_, err := s.db.Exec("DELETE FROM channel_users WHERE channel_id = ?", channelID)
	return err
}

// ClearNetworkChannelUsers removes all channel users for all channels in a network
func (s *Storage) ClearNetworkChannelUsers(networkID int64) error {
	_, err := s.db.Exec(`
		DELETE FROM channel_users 
		WHERE channel_id IN (SELECT id FROM channels WHERE network_id = ?)
	`, networkID)
	return err
}

// UpdateChannelUserNickname updates a user's nickname across all channels in a network
func (s *Storage) UpdateChannelUserNickname(networkID int64, oldNickname string, newNickname string) error {
	_, err := s.db.Exec(`
		UPDATE channel_users 
		SET nickname = ?, updated_at = CURRENT_TIMESTAMP
		WHERE nickname = ? 
		AND channel_id IN (SELECT id FROM channels WHERE network_id = ?)
	`, newNickname, oldNickname, networkID)
	return err
}

// GetPrivateMessages retrieves private messages for a network and user
// Private messages have channel_id IS NULL and user != '*'
// Returns both messages FROM the target user (received) and messages TO the target user (sent by currentUser)
// Uses case-insensitive matching for IRC nicknames
func (s *Storage) GetPrivateMessages(networkID int64, targetUser string, currentUser string, limit int) ([]Message, error) {
	var messages []Message
	// Normalize usernames to lowercase for case-insensitive comparison
	targetUserLower := strings.ToLower(targetUser)
	currentUserLower := strings.ToLower(currentUser)

	// Get messages FROM targetUser (received) OR messages TO targetUser sent by currentUser (sent)
	// For sent messages, we check the raw_line to identify the target (case-insensitive)
	err := s.db.Select(&messages,
		`SELECT * FROM messages 
		 WHERE network_id = ? AND channel_id IS NULL AND message_type IN ('privmsg', 'action')
		 AND (
		   LOWER(user) = ? OR 
		   (LOWER(user) = ? AND LOWER(raw_line) LIKE ?)
		 )
		 ORDER BY timestamp DESC 
		 LIMIT ?`,
		networkID, targetUserLower, currentUserLower, fmt.Sprintf("privmsg %s%%", targetUserLower), limit)

	if err != nil {
		return nil, fmt.Errorf("failed to get private messages: %w", err)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetPrivateMessageConversations retrieves a list of users with whom we have private message conversations
// Excludes the current user's own nickname (only shows conversations with other people)
// Uses case-insensitive grouping to consolidate conversations with different case variants of the same nickname
// If openOnly is true, only returns conversations where is_open = true
func (s *Storage) GetPrivateMessageConversations(networkID int64, currentUser string, openOnly bool) ([]string, error) {
	var users []string
	currentUserLower := strings.ToLower(currentUser)

	if openOnly {
		// Get only open PM conversations from the conversations table
		// Join with messages to get the original case variant of the nickname
		// Use the most recent message's user field to preserve original case
		err := s.db.Select(&users,
			`SELECT COALESCE(
				(SELECT m.user FROM messages m 
				 WHERE m.network_id = pmc.network_id 
				   AND m.channel_id IS NULL 
				   AND LOWER(m.user) = pmc.target_user 
				   AND m.message_type IN ('privmsg', 'action')
				 ORDER BY m.timestamp DESC 
				 LIMIT 1),
				pmc.target_user
			) as user
			 FROM private_message_conversations pmc
			 WHERE pmc.network_id = ? AND pmc.is_open = 1
			 ORDER BY pmc.updated_at DESC, pmc.created_at DESC`,
			networkID)
		if err != nil {
			return nil, fmt.Errorf("failed to get open private message conversations: %w", err)
		}
		return users, nil
	}

	// Group by lowercase username to consolidate case variants, but return the most recent case variant
	// We use MAX(user) to get one representative case variant per lowercase username
	err := s.db.Select(&users,
		`SELECT MAX(user) as user
		 FROM messages 
		 WHERE network_id = ? AND channel_id IS NULL AND user != '*' AND LOWER(user) != ? AND message_type IN ('privmsg', 'action')
		 GROUP BY LOWER(user)
		 ORDER BY MAX(timestamp) DESC`,
		networkID, currentUserLower)

	if err != nil {
		return nil, fmt.Errorf("failed to get private message conversations: %w", err)
	}

	return users, nil
}

// GetOrCreatePMConversation gets or creates a PM conversation record
// targetUser should be the other user in the conversation (normalized to lowercase)
// currentUser is used to determine the target user from messages if needed
func (s *Storage) GetOrCreatePMConversation(networkID int64, targetUser string, currentUser string) (*PrivateMessageConversation, error) {
	// Normalize target user to lowercase for case-insensitive matching
	targetUserLower := strings.ToLower(targetUser)

	var conv PrivateMessageConversation
	err := s.db.Get(&conv,
		"SELECT * FROM private_message_conversations WHERE network_id = ? AND target_user = ?",
		networkID, targetUserLower)

	if err == nil {
		// Conversation exists, return it
		return &conv, nil
	}

	// Check if it's a "no rows" error (conversation doesn't exist)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return nil, fmt.Errorf("failed to get PM conversation: %w", err)
	}

	// Conversation doesn't exist, create it
	now := time.Now()
	conv = PrivateMessageConversation{
		NetworkID:  networkID,
		TargetUser: targetUserLower,
		IsOpen:     true, // Auto-open new conversations
		CreatedAt:  now,
		UpdatedAt:  &now,
	}

	query := `INSERT INTO private_message_conversations (network_id, target_user, is_open, created_at, updated_at)
	          VALUES (:network_id, :target_user, :is_open, :created_at, :updated_at)`

	result, err := s.db.NamedExec(query, conv)
	if err != nil {
		return nil, fmt.Errorf("failed to create PM conversation: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get PM conversation ID: %w", err)
	}

	conv.ID = id
	return &conv, nil
}

// GetOpenPMConversations retrieves PM conversations where is_open = true
func (s *Storage) GetOpenPMConversations(networkID int64, currentUser string) ([]PrivateMessageConversation, error) {
	var conversations []PrivateMessageConversation
	err := s.db.Select(&conversations,
		`SELECT * FROM private_message_conversations 
		 WHERE network_id = ? AND is_open = 1
		 ORDER BY updated_at DESC, created_at DESC`,
		networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get open PM conversations: %w", err)
	}
	return conversations, nil
}

// UpdatePMConversationIsOpen updates the is_open status for a PM conversation
func (s *Storage) UpdatePMConversationIsOpen(networkID int64, targetUser string, isOpen bool) error {
	targetUserLower := strings.ToLower(targetUser)
	_, err := s.db.Exec(
		"UPDATE private_message_conversations SET is_open = ?, updated_at = CURRENT_TIMESTAMP WHERE network_id = ? AND target_user = ?",
		isOpen, networkID, targetUserLower)
	if err != nil {
		return fmt.Errorf("failed to update PM conversation is_open: %w", err)
	}
	return nil
}

// LastOpenPane represents the last open pane (channel or PM conversation)
// This is exported so it can be used by the app package
type LastOpenPane struct {
	NetworkID int64  `json:"network_id"`
	Type      string `json:"type"` // "channel" or "pm"
	Name      string `json:"name"` // Channel name or PM target user
}

// GetLastOpenPane retrieves the most recently updated open channel or PM conversation across all networks
func (s *Storage) GetLastOpenPane() (*LastOpenPane, error) {
	// Query for the most recently updated open channel
	var channel struct {
		NetworkID int64      `db:"network_id"`
		Name      string     `db:"name"`
		UpdatedAt *time.Time `db:"updated_at"`
	}
	channelQuery := `
		SELECT network_id, name, updated_at
		FROM channels
		WHERE is_open = 1
		ORDER BY COALESCE(updated_at, created_at) DESC, created_at DESC
		LIMIT 1
	`
	err := s.db.Get(&channel, channelQuery)
	channelFound := err == nil
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		// Unexpected error, return it
		return nil, fmt.Errorf("failed to query open channels: %w", err)
	}
	channelUpdatedAt := channel.UpdatedAt

	// Query for the most recently updated open PM conversation
	var pm struct {
		NetworkID  int64      `db:"network_id"`
		TargetUser string     `db:"target_user"`
		UpdatedAt  *time.Time `db:"updated_at"`
	}
	pmQuery := `
		SELECT network_id, target_user, updated_at
		FROM private_message_conversations
		WHERE is_open = 1
		ORDER BY COALESCE(updated_at, created_at) DESC, created_at DESC
		LIMIT 1
	`
	err = s.db.Get(&pm, pmQuery)
	pmFound := err == nil
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		// Unexpected error, return it
		return nil, fmt.Errorf("failed to query open PM conversations: %w", err)
	}
	pmUpdatedAt := pm.UpdatedAt

	// Compare timestamps to determine which is more recent
	if !channelFound && !pmFound {
		return nil, nil // No open panes
	}

	if !channelFound {
		// Only PM found
		return &LastOpenPane{
			NetworkID: pm.NetworkID,
			Type:      "pm",
			Name:      pm.TargetUser,
		}, nil
	}

	if !pmFound {
		// Only channel found
		return &LastOpenPane{
			NetworkID: channel.NetworkID,
			Type:      "channel",
			Name:      channel.Name,
		}, nil
	}

	// Both found, compare timestamps
	// Handle nil timestamps (shouldn't happen, but be safe)
	if channelUpdatedAt == nil && pmUpdatedAt == nil {
		// Both nil, prefer channel
		return &LastOpenPane{
			NetworkID: channel.NetworkID,
			Type:      "channel",
			Name:      channel.Name,
		}, nil
	}
	if channelUpdatedAt == nil {
		return &LastOpenPane{
			NetworkID: pm.NetworkID,
			Type:      "pm",
			Name:      pm.TargetUser,
		}, nil
	}
	if pmUpdatedAt == nil {
		return &LastOpenPane{
			NetworkID: channel.NetworkID,
			Type:      "channel",
			Name:      channel.Name,
		}, nil
	}

	// Both have timestamps, compare them
	if channelUpdatedAt.After(*pmUpdatedAt) || channelUpdatedAt.Equal(*pmUpdatedAt) {
		return &LastOpenPane{
			NetworkID: channel.NetworkID,
			Type:      "channel",
			Name:      channel.Name,
		}, nil
	}

	return &LastOpenPane{
		NetworkID: pm.NetworkID,
		Type:      "pm",
		Name:      pm.TargetUser,
	}, nil
}

// GetPluginConfig retrieves the configuration for a plugin
func (s *Storage) GetPluginConfig(name string) (*PluginConfig, error) {
	var config PluginConfig
	var configJSON sql.NullString
	var configSchemaJSON sql.NullString
	err := s.db.QueryRow("SELECT name, enabled, config, config_schema, created_at, updated_at FROM plugin_configs WHERE name = ?", name).Scan(
		&config.Name, &config.Enabled, &configJSON, &configSchemaJSON, &config.CreatedAt, &config.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			// Return default config if not found
			return &PluginConfig{
				Name:         name,
				Enabled:      true, // Default to enabled
				Config:       make(map[string]interface{}),
				ConfigSchema: make(map[string]interface{}),
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}, nil
		}
		return nil, fmt.Errorf("failed to get plugin config: %w", err)
	}

	// Decode JSON config
	if configJSON.Valid && configJSON.String != "" {
		if err := json.Unmarshal([]byte(configJSON.String), &config.Config); err != nil {
			logger.Log.Warn().Err(err).Str("plugin", name).Msg("Failed to decode plugin config JSON, using empty config")
			config.Config = make(map[string]interface{})
		}
	} else {
		config.Config = make(map[string]interface{})
	}

	// Decode JSON config_schema
	if configSchemaJSON.Valid && configSchemaJSON.String != "" {
		if err := json.Unmarshal([]byte(configSchemaJSON.String), &config.ConfigSchema); err != nil {
			logger.Log.Warn().Err(err).Str("plugin", name).Msg("Failed to decode plugin config_schema JSON, using empty schema")
			config.ConfigSchema = make(map[string]interface{})
		}
	} else {
		config.ConfigSchema = make(map[string]interface{})
	}

	return &config, nil
}

// SetPluginEnabled updates the enabled state for a plugin
func (s *Storage) SetPluginEnabled(name string, enabled bool) error {
	// Use INSERT OR REPLACE to handle both new and existing configs
	_, err := s.db.Exec(
		`INSERT INTO plugin_configs (name, enabled, created_at, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET enabled = ?, updated_at = CURRENT_TIMESTAMP`,
		name, enabled, enabled)
	if err != nil {
		return fmt.Errorf("failed to set plugin enabled state: %w", err)
	}
	return nil
}

// GetAllPluginConfigs retrieves all plugin configurations
func (s *Storage) GetAllPluginConfigs() (map[string]*PluginConfig, error) {
	rows, err := s.db.Query("SELECT name, enabled, config, config_schema, created_at, updated_at FROM plugin_configs")
	if err != nil {
		return nil, fmt.Errorf("failed to get all plugin configs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*PluginConfig)
	for rows.Next() {
		var config PluginConfig
		var configJSON sql.NullString
		var configSchemaJSON sql.NullString
		if err := rows.Scan(&config.Name, &config.Enabled, &configJSON, &configSchemaJSON, &config.CreatedAt, &config.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan plugin config: %w", err)
		}

		// Decode JSON config
		if configJSON.Valid && configJSON.String != "" {
			if err := json.Unmarshal([]byte(configJSON.String), &config.Config); err != nil {
				logger.Log.Warn().Err(err).Str("plugin", config.Name).Msg("Failed to decode plugin config JSON, using empty config")
				config.Config = make(map[string]interface{})
			}
		} else {
			config.Config = make(map[string]interface{})
		}

		// Decode JSON config_schema
		if configSchemaJSON.Valid && configSchemaJSON.String != "" {
			if err := json.Unmarshal([]byte(configSchemaJSON.String), &config.ConfigSchema); err != nil {
				logger.Log.Warn().Err(err).Str("plugin", config.Name).Msg("Failed to decode plugin config_schema JSON, using empty schema")
				config.ConfigSchema = make(map[string]interface{})
			}
		} else {
			config.ConfigSchema = make(map[string]interface{})
		}

		result[config.Name] = &config
	}

	return result, nil
}

// SetPluginConfig updates the configuration for a plugin
func (s *Storage) SetPluginConfig(name string, config map[string]interface{}) error {
	// Encode config as JSON
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to encode plugin config: %w", err)
	}

	// Use INSERT OR REPLACE to handle both new and existing configs
	// Preserve existing enabled state and config_schema
	_, err = s.db.Exec(
		`INSERT INTO plugin_configs (name, enabled, config, created_at, updated_at)
		 VALUES (?, 1, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET config = ?, updated_at = CURRENT_TIMESTAMP`,
		name, configJSON, configJSON)
	if err != nil {
		return fmt.Errorf("failed to set plugin config: %w", err)
	}
	return nil
}

// SetPluginConfigSchema stores the configuration schema for a plugin
func (s *Storage) SetPluginConfigSchema(name string, schema map[string]interface{}) error {
	// Encode config_schema as JSON
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to encode plugin config_schema: %w", err)
	}

	// Use INSERT OR REPLACE to handle both new and existing configs
	// Preserve existing enabled state and config
	_, err = s.db.Exec(
		`INSERT INTO plugin_configs (name, enabled, config_schema, created_at, updated_at)
		 VALUES (?, 1, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET config_schema = ?, updated_at = CURRENT_TIMESTAMP`,
		name, schemaJSON, schemaJSON)
	if err != nil {
		return fmt.Errorf("failed to set plugin config_schema: %w", err)
	}
	return nil
}
