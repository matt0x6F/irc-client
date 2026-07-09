package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
			messages = append(messages, normalizeForStore(msg))
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

			// Batch insert. NULLIF maps an empty pm_target/msgid to NULL: non-PM rows
			// stay out of the PM-keyed queries (and in the status pane), and msgid-less
			// rows stay out of the partial unique index (so they never collide). The
			// ON CONFLICT clause makes the live path idempotent against the msgid dedup
			// index — e.g. an echo and a CHATHISTORY replay of the same line.
			query := `INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line, pm_target, msgid, reply_msgid, channel_context)
			          VALUES (:network_id, :channel_id, :user, :message, :message_type, :timestamp, :raw_line, NULLIF(:pm_target, ''), NULLIF(:msgid, ''), NULLIF(:reply_msgid, ''), NULLIF(:channel_context, ''))
			          ON CONFLICT(network_id, COALESCE(channel_id,0), COALESCE(pm_target,''), msgid) WHERE msgid IS NOT NULL DO NOTHING`

			_, err := s.db.NamedExec(query, messages)
			if err != nil {
				logger.Log.Error().Err(err).Int("count", len(messages)).Msg("Error flushing messages")
				// Re-queue messages? For now, we'll lose them on error
			}
			return
		}
	}
}

// normalizeForStore canonicalizes a message before persistence. The timestamp is
// forced to UTC so the TIMESTAMP text column stays in a single, lexicographically
// monotonic format. SQLite has no native datetime type and compares the column as
// text, so mixed offsets (UTC server-time rows vs. local time.Now() rows) make
// `ORDER BY timestamp` and the `WHERE timestamp < ?` scrollback cursor
// (GetMessagesBeforeTime) chronologically wrong. Keeping every write in UTC keeps
// text order == chronological order.
func normalizeForStore(msg Message) Message {
	msg.Timestamp = msg.Timestamp.UTC()
	return msg
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

// WriteHistoryMessages bulk-inserts replayed CHATHISTORY messages, deduplicating
// against existing rows by IRCv3 msgid within the same conversation (the partial
// unique index on (network_id, channel_id/pm_target, msgid) — see
// idx_messages_conv_msgid). It returns the number of genuinely-new rows inserted —
// the caller uses a zero count to detect that the start of available history has
// been reached and stop paging. This is synchronous and bypasses the write buffer
// so the inserted rows are immediately queryable for the scrollback re-fetch.
func (s *Storage) WriteHistoryMessages(msgs []Message) (int, error) {
	if len(msgs) == 0 {
		return 0, nil
	}

	s.closedMu.RLock()
	if s.closed {
		s.closedMu.RUnlock()
		return 0, fmt.Errorf("storage is closed")
	}
	s.closedMu.RUnlock()

	// Same NULLIF + ON CONFLICT semantics as flushBuffer: msgid-less rows are
	// exempt from the dedup index; rows whose msgid already exists are skipped
	// (and excluded from RowsAffected, so the returned count is new rows only).
	query := `INSERT INTO messages (network_id, channel_id, user, message, message_type, timestamp, raw_line, pm_target, msgid, reply_msgid, channel_context)
	          VALUES (:network_id, :channel_id, :user, :message, :message_type, :timestamp, :raw_line, NULLIF(:pm_target, ''), NULLIF(:msgid, ''), NULLIF(:reply_msgid, ''), NULLIF(:channel_context, ''))
	          ON CONFLICT(network_id, COALESCE(channel_id,0), COALESCE(pm_target,''), msgid) WHERE msgid IS NOT NULL DO NOTHING`

	normalized := make([]Message, len(msgs))
	for i := range msgs {
		normalized[i] = normalizeForStore(msgs[i])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.NamedExec(query, normalized)
	if err != nil {
		return 0, fmt.Errorf("failed to write history messages: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to read history insert count: %w", err)
	}
	return int(inserted), nil
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

// GetMessageByMsgID returns the message with the given IRCv3 msgid on the
// network, used to resolve a +draft/reply parent for the quote preview / jump.
func (s *Storage) GetMessageByMsgID(networkID int64, msgid string) (Message, error) {
	row, err := s.queries.GetMessageByMsgID(context.Background(), db.GetMessageByMsgIDParams{
		NetworkID: networkID,
		Msgid:     convertToNullString(msgid),
	})
	if err != nil {
		return Message{}, fmt.Errorf("get message by msgid: %w", err)
	}
	return convertMessageFromDB(row), nil
}

// PinMessage pins a message (idempotent — re-pinning is a no-op)
func (s *Storage) PinMessage(messageID, networkID int64, channelID *int64, pinnedBy string) error {
	var channelIDNull sql.NullInt64
	if channelID != nil {
		channelIDNull = sql.NullInt64{Int64: *channelID, Valid: true}
	}
	err := s.queries.PinMessage(context.Background(), db.PinMessageParams{
		MessageID: messageID,
		NetworkID: networkID,
		ChannelID: channelIDNull,
		PinnedBy:  pinnedBy,
	})
	if err != nil {
		return fmt.Errorf("failed to pin message: %w", err)
	}
	return nil
}

// UnpinMessage removes a pin
func (s *Storage) UnpinMessage(messageID int64) error {
	if err := s.queries.UnpinMessage(context.Background(), messageID); err != nil {
		return fmt.Errorf("failed to unpin message: %w", err)
	}
	return nil
}

// GetPinnedMessages returns pinned messages for a network and channel (channelID nil = status/PM pane)
func (s *Storage) GetPinnedMessages(networkID int64, channelID *int64) ([]PinnedMessage, error) {
	if channelID != nil {
		rows, err := s.queries.GetPinnedMessagesWithChannel(context.Background(), db.GetPinnedMessagesWithChannelParams{
			NetworkID: networkID,
			ChannelID: sql.NullInt64{Int64: *channelID, Valid: true},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get pinned messages: %w", err)
		}
		pinned := make([]PinnedMessage, len(rows))
		for i, r := range rows {
			pinned[i] = convertPinnedMessageWithChannelFromDB(r)
		}
		return pinned, nil
	}

	rows, err := s.queries.GetPinnedMessagesWithoutChannel(context.Background(), networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pinned messages: %w", err)
	}
	pinned := make([]PinnedMessage, len(rows))
	for i, r := range rows {
		pinned[i] = convertPinnedMessageWithoutChannelFromDB(r)
	}
	return pinned, nil
}

// GetMessagesAround returns a window of messages around a target message id:
// up to `window` messages at or before the target, plus up to `window` after it,
// in chronological (ascending id) order.
func (s *Storage) GetMessagesAround(networkID int64, channelID *int64, targetID int64, window int) ([]Message, error) {
	var before, after []db.Message
	var err error

	if channelID != nil {
		channelIDNull := sql.NullInt64{Int64: *channelID, Valid: true}
		before, err = s.queries.GetMessagesBeforeWithChannel(context.Background(), db.GetMessagesBeforeWithChannelParams{
			NetworkID: networkID,
			ChannelID: channelIDNull,
			ID:        targetID,
			Limit:     int64(window),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get messages before target: %w", err)
		}
		after, err = s.queries.GetMessagesAfterWithChannel(context.Background(), db.GetMessagesAfterWithChannelParams{
			NetworkID: networkID,
			ChannelID: channelIDNull,
			ID:        targetID,
			Limit:     int64(window),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get messages after target: %w", err)
		}
	} else {
		before, err = s.queries.GetMessagesBeforeWithoutChannel(context.Background(), db.GetMessagesBeforeWithoutChannelParams{
			NetworkID: networkID,
			ID:        targetID,
			Limit:     int64(window),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get messages before target: %w", err)
		}
		after, err = s.queries.GetMessagesAfterWithoutChannel(context.Background(), db.GetMessagesAfterWithoutChannelParams{
			NetworkID: networkID,
			ID:        targetID,
			Limit:     int64(window),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get messages after target: %w", err)
		}
	}

	// `before` is DESC (newest first); reverse it to ascending, then append `after` (already ASC).
	messages := make([]Message, 0, len(before)+len(after))
	for i := len(before) - 1; i >= 0; i-- {
		messages = append(messages, convertMessageFromDB(before[i]))
	}
	for _, m := range after {
		messages = append(messages, convertMessageFromDB(m))
	}

	return messages, nil
}

// GetMessagesBefore returns up to `limit` messages strictly older than beforeID
// (exclusive), in chronological (ascending id) order. channelID nil = status pane.
// Used for scrollback pagination — loading history above the currently-loaded window.
func (s *Storage) GetMessagesBefore(networkID int64, channelID *int64, beforeID int64, limit int) ([]Message, error) {
	var dbMessages []db.Message
	var err error

	if channelID != nil {
		channelIDNull := sql.NullInt64{Int64: *channelID, Valid: true}
		dbMessages, err = s.queries.GetMessagesBeforeWithChannel(context.Background(), db.GetMessagesBeforeWithChannelParams{
			NetworkID: networkID,
			ChannelID: channelIDNull,
			ID:        beforeID - 1, // underlying query is id <= ?, so -1 makes it exclusive
			Limit:     int64(limit),
		})
	} else {
		dbMessages, err = s.queries.GetMessagesBeforeWithoutChannel(context.Background(), db.GetMessagesBeforeWithoutChannelParams{
			NetworkID: networkID,
			ID:        beforeID - 1,
			Limit:     int64(limit),
		})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get messages before: %w", err)
	}

	// Query returns DESC (newest first); reverse to ascending (chronological).
	messages := make([]Message, 0, len(dbMessages))
	for i := len(dbMessages) - 1; i >= 0; i-- {
		messages = append(messages, convertMessageFromDB(dbMessages[i]))
	}

	return messages, nil
}

// GetMessagesBeforeTime returns up to `limit` messages strictly older than
// `before` (by server-time timestamp), in chronological (ascending) order.
// Unlike GetMessagesBefore (id-keyed), this paginates by timestamp so it surfaces
// CHATHISTORY-backfilled rows, which are inserted with high ids but old timestamps.
// Target selection: a non-empty pmTarget selects a PM conversation; otherwise a
// non-nil channelID selects a channel; otherwise the status pane.
func (s *Storage) GetMessagesBeforeTime(networkID int64, channelID *int64, pmTarget string, before time.Time, limit int) ([]Message, error) {
	var dbMessages []db.Message
	var err error

	switch {
	case pmTarget != "":
		dbMessages, err = s.queries.GetMessagesBeforeTimePM(context.Background(), db.GetMessagesBeforeTimePMParams{
			NetworkID: networkID,
			PmTarget:  sql.NullString{String: strings.ToLower(pmTarget), Valid: true},
			Timestamp: before,
			Limit:     int64(limit),
		})
	case channelID != nil:
		dbMessages, err = s.queries.GetMessagesBeforeTimeWithChannel(context.Background(), db.GetMessagesBeforeTimeWithChannelParams{
			NetworkID: networkID,
			ChannelID: sql.NullInt64{Int64: *channelID, Valid: true},
			Timestamp: before,
			Limit:     int64(limit),
		})
	default:
		dbMessages, err = s.queries.GetMessagesBeforeTimeWithoutChannel(context.Background(), db.GetMessagesBeforeTimeWithoutChannelParams{
			NetworkID: networkID,
			Timestamp: before,
			Limit:     int64(limit),
		})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get messages before time: %w", err)
	}

	// Query returns DESC (newest first); reverse to ascending (chronological).
	messages := make([]Message, 0, len(dbMessages))
	for i := len(dbMessages) - 1; i >= 0; i-- {
		messages = append(messages, convertMessageFromDB(dbMessages[i]))
	}

	return messages, nil
}

// GetMessagesAfter returns up to `limit` messages strictly newer than afterID
// (exclusive), in chronological (ascending id) order. channelID nil = status pane.
// The newer-direction counterpart of GetMessagesBefore — used when scrolling down
// out of an anchored context window (e.g. after jumping to a pinned message).
func (s *Storage) GetMessagesAfter(networkID int64, channelID *int64, afterID int64, limit int) ([]Message, error) {
	var dbMessages []db.Message
	var err error

	if channelID != nil {
		channelIDNull := sql.NullInt64{Int64: *channelID, Valid: true}
		dbMessages, err = s.queries.GetMessagesAfterWithChannel(context.Background(), db.GetMessagesAfterWithChannelParams{
			NetworkID: networkID,
			ChannelID: channelIDNull,
			ID:        afterID, // underlying query is id > ?, already exclusive
			Limit:     int64(limit),
		})
	} else {
		dbMessages, err = s.queries.GetMessagesAfterWithoutChannel(context.Background(), db.GetMessagesAfterWithoutChannelParams{
			NetworkID: networkID,
			ID:        afterID,
			Limit:     int64(limit),
		})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get messages after: %w", err)
	}

	// Query already returns ASC (chronological).
	messages := make([]Message, len(dbMessages))
	for i, m := range dbMessages {
		messages[i] = convertMessageFromDB(m)
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

// UpdateNetworkColor sets (or clears, when color is nil) the rail color key.
func (s *Storage) UpdateNetworkColor(networkID int64, color *string) error {
	var v sql.NullString
	if color != nil {
		v = sql.NullString{String: *color, Valid: true}
	}
	return s.queries.UpdateNetworkColor(context.Background(), db.UpdateNetworkColorParams{Color: v, ID: networkID})
}

// UpdateNetworkSortOrder sets the rail sort position for a network.
func (s *Storage) UpdateNetworkSortOrder(networkID int64, order int64) error {
	return s.queries.UpdateNetworkSortOrder(context.Background(), db.UpdateNetworkSortOrderParams{SortOrder: order, ID: networkID})
}

// UpdateNetworkIcon records the on-disk path of a network's processed icon.
func (s *Storage) UpdateNetworkIcon(networkID int64, path string) error {
	return s.queries.UpdateNetworkIcon(context.Background(), db.UpdateNetworkIconParams{
		IconPath: sql.NullString{String: path, Valid: true}, ID: networkID,
	})
}

// ClearNetworkIcon clears a network's stored icon path (reverts to monogram).
func (s *Storage) ClearNetworkIcon(networkID int64) error {
	return s.queries.ClearNetworkIcon(context.Background(), networkID)
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

// UpdateChannelKey updates the channel key (+k) used to rejoin after a
// reconnect. An empty key clears it.
func (s *Storage) UpdateChannelKey(channelID int64, key string) error {
	err := s.queries.UpdateChannelKey(context.Background(), db.UpdateChannelKeyParams{
		Key: key,
		ID:  channelID,
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

// GetChannelUserModes returns the stored prefix modes (e.g. "@+") for a single user
// in a channel. Returns sql.ErrNoRows if the user is not currently tracked.
func (s *Storage) GetChannelUserModes(channelID int64, nickname string) (string, error) {
	modes, err := s.queries.GetChannelUserModes(context.Background(), db.GetChannelUserModesParams{
		ChannelID: channelID,
		LOWER:     nickname,
	})
	if err != nil {
		return "", err
	}
	if modes.Valid {
		return modes.String, nil
	}
	return "", nil
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

// GetPrivateMessages retrieves the private-message conversation with targetUser.
// PM rows carry their conversation peer in pm_target (set when written), so both
// sent and received messages are matched by a single case-insensitive equality.
// The currentUser parameter is retained for API compatibility but no longer used.
func (s *Storage) GetPrivateMessages(networkID int64, targetUser string, currentUser string, limit int) ([]Message, error) {
	_ = currentUser
	targetUserLower := strings.ToLower(targetUser)

	dbMessages, err := s.queries.GetPrivateMessages(context.Background(), db.GetPrivateMessagesParams{
		NetworkID: networkID,
		PmTarget:  sql.NullString{String: targetUserLower, Valid: true},
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

// GetOrCreatePMConversation gets or creates a PM conversation record.
// targetUser should be the other user in the conversation (normalized to lowercase).
// currentUser is used to determine the target user from messages if needed.
//
// The returned bool reports whether a NEW conversation row was created (true) vs.
// an existing one returned (false). Callers use this to announce a sidebar change
// (channels.changed) only when a fresh DM entry actually appeared, so the DM list
// refreshes for a new peer without re-fetching on every message to an open chat.
func (s *Storage) GetOrCreatePMConversation(networkID int64, targetUser string, currentUser string) (*PrivateMessageConversation, bool, error) {
	// Normalize target user to lowercase for case-insensitive matching
	targetUserLower := strings.ToLower(targetUser)

	dbConv, err := s.queries.GetPMConversation(context.Background(), db.GetPMConversationParams{
		NetworkID:  networkID,
		TargetUser: targetUserLower,
	})

	if err == nil {
		// Conversation exists, return it
		conv := convertPMConversationFromDB(dbConv)
		return &conv, false, nil
	}

	// Check if it's a "no rows" error (conversation doesn't exist)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return nil, false, fmt.Errorf("failed to get PM conversation: %w", err)
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
		return nil, false, fmt.Errorf("failed to create PM conversation: %w", err)
	}

	conv = convertPMConversationFromDB(dbConv)
	return &conv, true, nil
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

// GetSetting returns the value for a key in the settings key/value store.
// A missing key is not an error: it returns an empty string so callers can fall
// back to their own defaults.
func (s *Storage) GetSetting(key string) (string, error) {
	value, err := s.queries.GetSetting(context.Background(), key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get setting %q: %w", key, err)
	}
	return value, nil
}

// SetSetting upserts a key/value pair in the settings store.
func (s *Storage) SetSetting(key, value string) error {
	if err := s.queries.SetSetting(context.Background(), db.SetSettingParams{
		Key:   key,
		Value: value,
	}); err != nil {
		return fmt.Errorf("failed to set setting %q: %w", key, err)
	}
	return nil
}

// GetLinkPreview returns a cached preview if present and not older than
// ttlSeconds relative to nowUnix. A stale row reports (nil, false, nil) and is
// left in place (overwritten on the next fetch, swept by PruneLinkPreviews).
func (s *Storage) GetLinkPreview(url string, nowUnix, ttlSeconds int64) (*CachedPreview, bool, error) {
	row, err := s.queries.GetLinkPreview(context.Background(), url)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get link preview %q: %w", url, err)
	}
	if nowUnix-row.FetchedAt > ttlSeconds {
		return nil, false, nil
	}
	return &CachedPreview{
		URL:          row.Url,
		Status:       row.Status,
		Title:        row.Title,
		Description:  row.Description,
		SiteName:     row.SiteName,
		ImageDataURI: row.ImageData,
		FetchedAt:    row.FetchedAt,
	}, true, nil
}

// UpsertLinkPreview stores or refreshes a preview row.
func (s *Storage) UpsertLinkPreview(p CachedPreview) error {
	if err := s.queries.UpsertLinkPreview(context.Background(), db.UpsertLinkPreviewParams{
		Url:         p.URL,
		Status:      p.Status,
		Title:       p.Title,
		Description: p.Description,
		SiteName:    p.SiteName,
		ImageData:   p.ImageDataURI,
		FetchedAt:   p.FetchedAt,
	}); err != nil {
		return fmt.Errorf("failed to upsert link preview %q: %w", p.URL, err)
	}
	return nil
}

// PruneLinkPreviews keeps only the maxRows most-recently-fetched rows.
func (s *Storage) PruneLinkPreviews(maxRows int) error {
	if err := s.queries.PruneLinkPreviewsToLimit(context.Background(), int64(maxRows)); err != nil {
		return fmt.Errorf("failed to prune link previews: %w", err)
	}
	return nil
}

// UpsertSTSPolicy stores (or refreshes) an STS policy for a host. expiresAt is a
// unix timestamp in seconds. Called only after a policy is advertised over TLS.
func (s *Storage) UpsertSTSPolicy(hostname string, port int, expiresAt int64) error {
	if err := s.queries.UpsertSTSPolicy(context.Background(), db.UpsertSTSPolicyParams{
		Hostname:  hostname,
		Port:      int64(port),
		ExpiresAt: expiresAt,
	}); err != nil {
		return fmt.Errorf("failed to upsert STS policy for %q: %w", hostname, err)
	}
	return nil
}

// GetSTSPolicy returns the active (non-expired) STS policy for a host. The bool is
// false when no policy exists OR the stored policy has expired (a stale row is left
// in place and simply treated as absent — it is overwritten on the next secure
// advertisement and pruned by PruneExpiredSTSPolicies). nowUnix is the current unix
// time in seconds, passed in so callers and tests control the clock.
func (s *Storage) GetSTSPolicy(hostname string, nowUnix int64) (*STSPolicy, bool, error) {
	row, err := s.queries.GetSTSPolicy(context.Background(), hostname)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get STS policy for %q: %w", hostname, err)
	}
	if row.ExpiresAt <= nowUnix {
		return nil, false, nil
	}
	policy := convertSTSPolicyFromDB(row)
	return &policy, true, nil
}

// GetSTSPolicies returns every stored STS policy (including expired ones) for
// display/management in the UI.
func (s *Storage) GetSTSPolicies() ([]STSPolicy, error) {
	rows, err := s.queries.GetSTSPolicies(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get STS policies: %w", err)
	}
	policies := make([]STSPolicy, len(rows))
	for i, r := range rows {
		policies[i] = convertSTSPolicyFromDB(r)
	}
	return policies, nil
}

// DeleteSTSPolicy removes the STS policy for a host (used by duration=0 clears and
// the user-facing "Clear STS policy" control).
func (s *Storage) DeleteSTSPolicy(hostname string) error {
	if err := s.queries.DeleteSTSPolicy(context.Background(), hostname); err != nil {
		return fmt.Errorf("failed to delete STS policy for %q: %w", hostname, err)
	}
	return nil
}

// AddMonitoredNick adds a nick to a network's durable MONITOR buddy list
// (idempotent — re-adding an existing nick is a no-op).
func (s *Storage) AddMonitoredNick(networkID int64, nickname string) error {
	if err := s.queries.AddMonitoredNick(context.Background(), db.AddMonitoredNickParams{
		NetworkID: networkID,
		Nickname:  nickname,
	}); err != nil {
		return fmt.Errorf("failed to add monitored nick %q: %w", nickname, err)
	}
	return nil
}

// RemoveMonitoredNick removes a nick from a network's MONITOR buddy list.
func (s *Storage) RemoveMonitoredNick(networkID int64, nickname string) error {
	if err := s.queries.RemoveMonitoredNick(context.Background(), db.RemoveMonitoredNickParams{
		NetworkID: networkID,
		Nickname:  nickname,
	}); err != nil {
		return fmt.Errorf("failed to remove monitored nick %q: %w", nickname, err)
	}
	return nil
}

// GetMonitoredNicks returns the nicks on a network's MONITOR buddy list, sorted.
func (s *Storage) GetMonitoredNicks(networkID int64) ([]string, error) {
	rows, err := s.queries.GetMonitoredNicks(context.Background(), networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitored nicks: %w", err)
	}
	nicks := make([]string, len(rows))
	for i, r := range rows {
		nicks[i] = r.Nickname
	}
	return nicks, nil
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

// SetScriptEnabled persists the enabled/disabled state for a script. A disabled
// script (enabled=false) gets a row; an enabled script gets enabled=1 (the default),
// so callers can toggle either direction idempotently.
func (s *Storage) SetScriptEnabled(id string, enabled bool) error {
	enabledInt := int64(1)
	if !enabled {
		enabledInt = 0
	}
	if err := s.queries.UpsertScriptEnabled(context.Background(), db.UpsertScriptEnabledParams{
		ScriptID: id,
		Enabled:  enabledInt,
	}); err != nil {
		return fmt.Errorf("failed to set script enabled state for %q: %w", id, err)
	}
	return nil
}

// DisabledScripts returns the set of script IDs that have been explicitly
// disabled. A missing row means enabled (the default), so only disabled scripts
// appear here. The returned map is keyed by script_id; a present key means disabled.
func (s *Storage) DisabledScripts() (map[string]bool, error) {
	ids, err := s.queries.ListDisabledScripts(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list disabled scripts: %w", err)
	}
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result, nil
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
