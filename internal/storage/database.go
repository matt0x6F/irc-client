package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/matt0x6f/irc-client/internal/logger"
	db "github.com/matt0x6f/irc-client/internal/storage/generated"
	_ "github.com/mattn/go-sqlite3"
)

// Storage handles database operations
type Storage struct {
	db            *sqlx.DB // Keep for migrations and batch operations
	queries       *db.Queries
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
	sqlxDB, err := sqlx.Connect("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	sqlxDB.SetMaxOpenConns(1) // SQLite works best with single connection in WAL mode
	sqlxDB.SetMaxIdleConns(1)
	sqlxDB.SetConnMaxLifetime(time.Hour)

	// Create SQLC queries instance
	queries := db.New(sqlxDB.DB)

	storage := &Storage{
		db:            sqlxDB,
		queries:       queries,
		writeBuffer:   make(chan Message, bufferSize),
		bufferSize:    bufferSize,
		flushInterval: flushInterval,
		stopCh:        make(chan struct{}),
	}

	// Run migrations
	if err := Migrate(sqlxDB); err != nil {
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
		// Buffer still full after flush, try direct insert using SQLC
		params := convertMessageToDBCreateParams(msg)
		_, err := s.queries.CreateMessage(context.Background(), params)
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

	// Direct insert to database using SQLC
	params := convertMessageToDBCreateParams(msg)
	_, err := s.queries.CreateMessage(context.Background(), params)
	return err
}

// GetMessages retrieves messages for a network and channel
func (s *Storage) GetMessages(networkID int64, channelID *int64, limit int) ([]Message, error) {
	var dbMessages []db.Message
	var err error

	if channelID != nil {
		var channelIDNull sql.NullInt64
		channelIDNull = sql.NullInt64{Int64: *channelID, Valid: true}
		dbMessages, err = s.queries.GetMessagesWithChannel(context.Background(), db.GetMessagesWithChannelParams{
			NetworkID: networkID,
			ChannelID: channelIDNull,
			Limit:     int64(limit),
		})
	} else {
		dbMessages, err = s.queries.GetMessagesWithoutChannel(context.Background(), db.GetMessagesWithoutChannelParams{
			NetworkID: networkID,
			Limit:     int64(limit),
		})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	messages := make([]Message, len(dbMessages))
	for i, m := range dbMessages {
		messages[i] = convertMessageFromDB(m)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// CreateNetwork creates a new network configuration
func (s *Storage) CreateNetwork(network *Network) error {
	params := convertNetworkToDBCreateParams(network)
	dbNetwork, err := s.queries.CreateNetwork(context.Background(), params)
	if err != nil {
		return fmt.Errorf("failed to create network: %w", err)
	}
	network.ID = dbNetwork.ID
	return nil
}

// GetNetworks retrieves all networks
func (s *Storage) GetNetworks() ([]Network, error) {
	dbNetworks, err := s.queries.GetNetworks(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get networks: %w", err)
	}
	networks := make([]Network, len(dbNetworks))
	for i, n := range dbNetworks {
		networks[i] = convertNetworkFromDB(n)
	}
	return networks, nil
}

// GetNetwork retrieves a network by ID
func (s *Storage) GetNetwork(networkID int64) (*Network, error) {
	dbNetwork, err := s.queries.GetNetwork(context.Background(), networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get network: %w", err)
	}
	network := convertNetworkFromDB(dbNetwork)
	return &network, nil
}

// UpdateNetwork updates a network configuration
func (s *Storage) UpdateNetwork(network *Network) error {
	params := convertNetworkToDBUpdateParams(network)
	err := s.queries.UpdateNetwork(context.Background(), params)
	if err != nil {
		return fmt.Errorf("failed to update network: %w", err)
	}
	return nil
}

// UpdateNetworkAutoConnect updates the auto-connect setting for a network
func (s *Storage) UpdateNetworkAutoConnect(networkID int64, autoConnect bool) error {
	err := s.queries.UpdateNetworkAutoConnect(context.Background(), db.UpdateNetworkAutoConnectParams{
		AutoConnect: autoConnect,
		ID:          networkID,
	})
	return err
}

// DeleteNetwork deletes a network (cascade will delete servers)
func (s *Storage) DeleteNetwork(networkID int64) error {
	err := s.queries.DeleteNetwork(context.Background(), networkID)
	return err
}

// GetServers retrieves all server addresses for a network, ordered by priority
func (s *Storage) GetServers(networkID int64) ([]Server, error) {
	dbServers, err := s.queries.GetServers(context.Background(), networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get servers: %w", err)
	}
	servers := make([]Server, len(dbServers))
	for i, srv := range dbServers {
		servers[i] = convertServerFromDB(srv)
	}
	return servers, nil
}

// CreateServer creates a new server address
func (s *Storage) CreateServer(server *Server) error {
	params := convertServerToDBCreateParams(server)
	dbServer, err := s.queries.CreateServer(context.Background(), params)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	server.ID = dbServer.ID
	return nil
}

// UpdateServer updates a server address
func (s *Storage) UpdateServer(server *Server) error {
	params := convertServerToDBUpdateParams(server)
	err := s.queries.UpdateServer(context.Background(), params)
	if err != nil {
		return fmt.Errorf("failed to update server: %w", err)
	}
	return nil
}

// DeleteServer deletes a server address
func (s *Storage) DeleteServer(serverID int64) error {
	err := s.queries.DeleteServer(context.Background(), serverID)
	return err
}

// DeleteAllServers deletes all server addresses for a network
func (s *Storage) DeleteAllServers(networkID int64) error {
	err := s.queries.DeleteAllServers(context.Background(), networkID)
	return err
}

// CreateChannel creates a new channel
func (s *Storage) CreateChannel(channel *Channel) error {
	params := convertChannelToDBCreateParams(channel)
	dbChannel, err := s.queries.CreateChannel(context.Background(), params)
	if err != nil {
		return fmt.Errorf("failed to create channel: %w", err)
	}
	channel.ID = dbChannel.ID
	return nil
}

// GetChannels retrieves channels for a network
func (s *Storage) GetChannels(networkID int64) ([]Channel, error) {
	dbChannels, err := s.queries.GetChannels(context.Background(), networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channels: %w", err)
	}
	channels := make([]Channel, len(dbChannels))
	for i, c := range dbChannels {
		channels[i] = convertChannelFromDB(c)
	}
	return channels, nil
}

// GetJoinedChannels retrieves channels for a network where the specified nickname is a member
func (s *Storage) GetJoinedChannels(networkID int64, nickname string) ([]Channel, error) {
	dbChannels, err := s.queries.GetJoinedChannels(context.Background(), db.GetJoinedChannelsParams{
		NetworkID: networkID,
		LOWER:     nickname,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get joined channels: %w", err)
	}
	channels := make([]Channel, len(dbChannels))
	for i, c := range dbChannels {
		channels[i] = convertChannelFromDB(c)
	}
	return channels, nil
}

// GetChannelByName retrieves a channel by network ID and channel name
// Channel names are case-insensitive for IRC channels (channels starting with # or &)
func (s *Storage) GetChannelByName(networkID int64, channelName string) (*Channel, error) {
	dbChannel, err := s.queries.GetChannelByName(context.Background(), db.GetChannelByNameParams{
		NetworkID: networkID,
		LOWER:     channelName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get channel: %w", err)
	}
	channel := convertChannelFromDB(dbChannel)
	return &channel, nil
}

// UpdateChannelTopic updates the topic for a channel
func (s *Storage) UpdateChannelTopic(channelID int64, topic string) error {
	err := s.queries.UpdateChannelTopic(context.Background(), db.UpdateChannelTopicParams{
		Topic: convertToNullString(topic),
		ID:    channelID,
	})
	return err
}

// UpdateChannelModes updates the modes for a channel
func (s *Storage) UpdateChannelModes(channelID int64, modes string) error {
	err := s.queries.UpdateChannelModes(context.Background(), db.UpdateChannelModesParams{
		Modes: convertToNullString(modes),
		ID:    channelID,
	})
	return err
}

// UpdateChannelAutoJoin updates the auto-join setting for a channel
func (s *Storage) UpdateChannelAutoJoin(channelID int64, autoJoin bool) error {
	err := s.queries.UpdateChannelAutoJoin(context.Background(), db.UpdateChannelAutoJoinParams{
		AutoJoin: autoJoin,
		ID:       channelID,
	})
	return err
}

// UpdateChannelIsOpen updates the is_open state for a channel
func (s *Storage) UpdateChannelIsOpen(channelID int64, isOpen bool) error {
	err := s.queries.UpdateChannelIsOpen(context.Background(), db.UpdateChannelIsOpenParams{
		IsOpen: isOpen,
		ID:     channelID,
	})
	return err
}

// GetOpenChannels retrieves channels for a network where the dialog is open (is_open=true) or where the user is joined
func (s *Storage) GetOpenChannels(networkID int64, nickname string) ([]Channel, error) {
	dbChannels, err := s.queries.GetOpenChannels(context.Background(), db.GetOpenChannelsParams{
		LOWER:     nickname,
		NetworkID: networkID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get open channels: %w", err)
	}
	channels := make([]Channel, len(dbChannels))
	for i, c := range dbChannels {
		channels[i] = convertChannelFromDB(c)
	}
	return channels, nil
}

// GetChannelUsers retrieves all users for a channel
func (s *Storage) GetChannelUsers(channelID int64) ([]ChannelUser, error) {
	dbUsers, err := s.queries.GetChannelUsers(context.Background(), channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel users: %w", err)
	}
	users := make([]ChannelUser, len(dbUsers))
	for i, u := range dbUsers {
		users[i] = convertChannelUserFromDB(u)
	}
	return users, nil
}

// AddChannelUser adds or updates a user in a channel
func (s *Storage) AddChannelUser(channelID int64, nickname string, modes string) error {
	err := s.queries.AddChannelUser(context.Background(), db.AddChannelUserParams{
		ChannelID: channelID,
		Nickname:  nickname,
		Modes:     convertToNullString(modes),
	})
	return err
}

// RemoveChannelUser removes a user from a channel
func (s *Storage) RemoveChannelUser(channelID int64, nickname string) error {
	err := s.queries.RemoveChannelUser(context.Background(), db.RemoveChannelUserParams{
		ChannelID: channelID,
		LOWER:     nickname,
	})
	return err
}

// ClearChannelUsers removes all users from a channel
func (s *Storage) ClearChannelUsers(channelID int64) error {
	err := s.queries.ClearChannelUsers(context.Background(), channelID)
	return err
}

// ClearNetworkChannelUsers removes all channel users for all channels in a network
func (s *Storage) ClearNetworkChannelUsers(networkID int64) error {
	err := s.queries.ClearNetworkChannelUsers(context.Background(), networkID)
	return err
}

// UpdateChannelUserNickname updates a user's nickname across all channels in a network
func (s *Storage) UpdateChannelUserNickname(networkID int64, oldNickname string, newNickname string) error {
	err := s.queries.UpdateChannelUserNickname(context.Background(), db.UpdateChannelUserNicknameParams{
		Nickname:  newNickname,
		LOWER:     oldNickname,
		NetworkID: networkID,
	})
	return err
}

// GetPrivateMessages retrieves private messages for a network and user
// Private messages have channel_id IS NULL and user != '*'
// Returns both messages FROM the target user (received) and messages TO the target user (sent by currentUser)
// Uses case-insensitive matching for IRC nicknames
func (s *Storage) GetPrivateMessages(networkID int64, targetUser string, currentUser string, limit int) ([]Message, error) {
	// Normalize usernames to lowercase for case-insensitive comparison
	targetUserLower := strings.ToLower(targetUser)
	currentUserLower := strings.ToLower(currentUser)

	// Get messages FROM targetUser (received) OR messages TO targetUser sent by currentUser (sent)
	// For sent messages, we check the raw_line to identify the target (case-insensitive)
	dbMessages, err := s.queries.GetPrivateMessages(context.Background(), db.GetPrivateMessagesParams{
		NetworkID: networkID,
		User:      targetUserLower,
		User_2:    currentUserLower,
		RawLine:   sql.NullString{String: fmt.Sprintf("privmsg %s%%", targetUserLower), Valid: true},
		Limit:     int64(limit),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get private messages: %w", err)
	}

	messages := make([]Message, len(dbMessages))
	for i, m := range dbMessages {
		messages[i] = convertMessageFromDB(m)
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
	currentUserLower := strings.ToLower(currentUser)

	if openOnly {
		// Get only open PM conversations from the conversations table
		users, err := s.queries.GetPrivateMessageConversationsOpen(context.Background(), networkID)
		if err != nil {
			return nil, fmt.Errorf("failed to get open private message conversations: %w", err)
		}
		return users, nil
	}

	// Group by lowercase username to consolidate case variants, but return the most recent case variant
	// We use MAX(user) to get one representative case variant per lowercase username
	dbUsers, err := s.queries.GetPrivateMessageConversationsAll(context.Background(), db.GetPrivateMessageConversationsAllParams{
		NetworkID: networkID,
		User:      currentUserLower,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get private message conversations: %w", err)
	}

	users := make([]string, len(dbUsers))
	for i, u := range dbUsers {
		if userStr, ok := u.(string); ok {
			users[i] = userStr
		} else {
			return nil, fmt.Errorf("unexpected type in GetPrivateMessageConversationsAll result")
		}
	}

	return users, nil
}

// GetOrCreatePMConversation gets or creates a PM conversation record
// targetUser should be the other user in the conversation (normalized to lowercase)
// currentUser is used to determine the target user from messages if needed
func (s *Storage) GetOrCreatePMConversation(networkID int64, targetUser string, currentUser string) (*PrivateMessageConversation, error) {
	// Normalize target user to lowercase for case-insensitive matching
	targetUserLower := strings.ToLower(targetUser)

	dbConv, err := s.queries.GetPMConversation(context.Background(), db.GetPMConversationParams{
		NetworkID:  networkID,
		TargetUser: targetUserLower,
	})

	if err == nil {
		// Conversation exists, return it
		conv := convertPMConversationFromDB(dbConv)
		return &conv, nil
	}

	// Check if it's a "no rows" error (conversation doesn't exist)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return nil, fmt.Errorf("failed to get PM conversation: %w", err)
	}

	// Conversation doesn't exist, create it
	now := time.Now()
	conv := PrivateMessageConversation{
		NetworkID:  networkID,
		TargetUser: targetUserLower,
		IsOpen:     true, // Auto-open new conversations
		CreatedAt:  now,
		UpdatedAt:  &now,
	}

	params := convertPMConversationToDBCreateParams(&conv)
	dbConv, err = s.queries.CreatePMConversation(context.Background(), params)
	if err != nil {
		return nil, fmt.Errorf("failed to create PM conversation: %w", err)
	}

	conv = convertPMConversationFromDB(dbConv)
	return &conv, nil
}

// GetOpenPMConversations retrieves PM conversations where is_open = true
func (s *Storage) GetOpenPMConversations(networkID int64, currentUser string) ([]PrivateMessageConversation, error) {
	dbConversations, err := s.queries.GetOpenPMConversations(context.Background(), networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get open PM conversations: %w", err)
	}
	conversations := make([]PrivateMessageConversation, len(dbConversations))
	for i, c := range dbConversations {
		conversations[i] = convertPMConversationFromDB(c)
	}
	return conversations, nil
}

// UpdatePMConversationIsOpen updates the is_open status for a PM conversation
func (s *Storage) UpdatePMConversationIsOpen(networkID int64, targetUser string, isOpen bool) error {
	targetUserLower := strings.ToLower(targetUser)
	err := s.queries.UpdatePMConversationIsOpen(context.Background(), db.UpdatePMConversationIsOpenParams{
		IsOpen:     isOpen,
		NetworkID:  networkID,
		TargetUser: targetUserLower,
	})
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
	channel, err := s.queries.GetLastOpenChannel(context.Background())
	channelFound := err == nil
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		// Unexpected error, return it
		return nil, fmt.Errorf("failed to query open channels: %w", err)
	}
	var channelUpdatedAt *time.Time
	if channelFound && channel.UpdatedAt.Valid {
		channelUpdatedAt = &channel.UpdatedAt.Time
	}

	// Query for the most recently updated open PM conversation
	pm, err := s.queries.GetLastOpenPM(context.Background())
	pmFound := err == nil
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		// Unexpected error, return it
		return nil, fmt.Errorf("failed to query open PM conversations: %w", err)
	}
	var pmUpdatedAt *time.Time
	if pmFound && pm.UpdatedAt.Valid {
		pmUpdatedAt = &pm.UpdatedAt.Time
	}

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

// SearchResult extends Message with additional context for search results
type SearchResult struct {
	Message
	ChannelName string `db:"channel_name" json:"channel_name"`
	NetworkName string `db:"network_name" json:"network_name"`
}

// SearchMessages performs full-text search across messages using FTS5
func (s *Storage) SearchMessages(query string, networkID *int64, limit int) ([]SearchResult, error) {
	if query == "" {
		return []SearchResult{}, nil
	}
	if limit <= 0 {
		limit = 50
	}

	// Sanitize the query for FTS5: wrap terms in quotes to avoid syntax errors
	// from special characters, and add * for prefix matching
	sanitized := sanitizeFTS5Query(query)

	var results []SearchResult
	var err error

	if networkID != nil {
		err = s.db.Select(&results, `
			SELECT m.id, m.network_id, m.channel_id, m.user, m.message, m.message_type, m.timestamp, m.raw_line,
				COALESCE(c.name, '') as channel_name,
				COALESCE(n.name, '') as network_name
			FROM messages m
			JOIN messages_fts ON messages_fts.rowid = m.id
			LEFT JOIN channels c ON m.channel_id = c.id
			LEFT JOIN networks n ON m.network_id = n.id
			WHERE messages_fts MATCH ?
			AND m.network_id = ?
			ORDER BY m.timestamp DESC
			LIMIT ?
		`, sanitized, *networkID, limit)
	} else {
		err = s.db.Select(&results, `
			SELECT m.id, m.network_id, m.channel_id, m.user, m.message, m.message_type, m.timestamp, m.raw_line,
				COALESCE(c.name, '') as channel_name,
				COALESCE(n.name, '') as network_name
			FROM messages m
			JOIN messages_fts ON messages_fts.rowid = m.id
			LEFT JOIN channels c ON m.channel_id = c.id
			LEFT JOIN networks n ON m.network_id = n.id
			WHERE messages_fts MATCH ?
			ORDER BY m.timestamp DESC
			LIMIT ?
		`, sanitized, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}

	return results, nil
}

// sanitizeFTS5Query sanitizes a user query for FTS5 MATCH syntax
func sanitizeFTS5Query(query string) string {
	// Trim whitespace
	query = strings.TrimSpace(query)
	if query == "" {
		return query
	}

	// Split into words and wrap each in quotes, adding * for prefix matching
	words := strings.Fields(query)
	quoted := make([]string, 0, len(words))
	for _, word := range words {
		// Remove any existing quotes to avoid injection
		clean := strings.ReplaceAll(word, "\"", "")
		if clean != "" {
			quoted = append(quoted, "\""+clean+"\"")
		}
	}

	return strings.Join(quoted, " ")
}

// GetPluginConfig retrieves the configuration for a plugin
func (s *Storage) GetPluginConfig(name string) (*PluginConfig, error) {
	dbConfig, err := s.queries.GetPluginConfig(context.Background(), name)
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

	return convertPluginConfigFromDB(dbConfig)
}

// SetPluginEnabled updates the enabled state for a plugin
func (s *Storage) SetPluginEnabled(name string, enabled bool) error {
	err := s.queries.SetPluginEnabled(context.Background(), db.SetPluginEnabledParams{
		Name:    name,
		Enabled: enabled,
	})
	if err != nil {
		return fmt.Errorf("failed to set plugin enabled state: %w", err)
	}
	return nil
}

// GetAllPluginConfigs retrieves all plugin configurations
func (s *Storage) GetAllPluginConfigs() (map[string]*PluginConfig, error) {
	dbConfigs, err := s.queries.GetAllPluginConfigs(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get all plugin configs: %w", err)
	}

	result := make(map[string]*PluginConfig)
	for _, dbConfig := range dbConfigs {
		config, err := convertPluginConfigFromDB(dbConfig)
		if err != nil {
			logger.Log.Warn().Err(err).Str("plugin", dbConfig.Name).Msg("Failed to convert plugin config, skipping")
			continue
		}
		result[config.Name] = config
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

	err = s.queries.SetPluginConfig(context.Background(), db.SetPluginConfigParams{
		Name:   name,
		Config: configJSON,
	})
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

	err = s.queries.SetPluginConfigSchema(context.Background(), db.SetPluginConfigSchemaParams{
		Name:         name,
		ConfigSchema: schemaJSON,
	})
	if err != nil {
		return fmt.Errorf("failed to set plugin config_schema: %w", err)
	}
	return nil
}
