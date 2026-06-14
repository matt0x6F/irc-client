package irc

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/constants"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/matt0x6f/irc-client/internal/validation"
)

// requestedCaps is the list of IRCv3 capabilities this client wants to negotiate.
var requestedCaps = []string{"sasl", "server-time", "echo-message", "message-tags"}

// IRCClient manages IRC connections
type IRCClient struct {
	conn                *ircevent.Connection
	eventBus            *events.EventBus
	storage             *storage.Storage
	networkID           int64
	network             *storage.Network
	mu                  sync.RWMutex
	connected           bool
	lastMessageTime     time.Time // Last time we received any message from server (including PING). Informational only — liveness is owned by the library pingLoop, not this field.
	saslEnabled         bool
	saslMechanism       string
	saslUsername        string
	saslPassword        string
	saslInProgress      bool
	saslAuthenticated   bool
	saslCapRequested    bool
	saslCapAcknowledged bool
	scramState          *SCRAMState
	namesInProgress     map[string]bool       // Track channels currently receiving NAMES list
	namesMu             sync.Mutex            // Mutex for namesInProgress map
	serverCapabilities  *ServerCapabilities   // Server capabilities from ISUPPORT
	whoisInProgress     map[string]*WhoisInfo // Track WHOIS requests in progress (key: nickname)
	whoisMu             sync.Mutex            // Mutex for whoisInProgress map
	enabledCaps         map[string]bool       // IRCv3 capabilities granted by the server
	capNegotiationDone  bool                  // Whether CAP negotiation has finished
	channelListItems    []ChannelListItem     // Temporary storage for LIST response
	channelListMu       sync.Mutex            // Mutex for channelListItems
	banLists            map[string][]BanEntry // Per-channel ban entries collected between 367 and 368
	banListsMu          sync.Mutex            // Mutex for banLists
	rateLimiter         *RateLimiter          // Rate limiter for outgoing messages
	desiredNick         string                // The original nickname the user wanted
	nickAttempt         int                   // Current nick collision retry attempt (0 = first try with underscore)
}

// ServerCapabilities stores parsed ISUPPORT information
type ServerCapabilities struct {
	Prefix       map[rune]rune // Map prefix char to mode char (e.g., '@' -> 'o', '+' -> 'v')
	PrefixString string        // Raw PREFIX string (e.g., "(ov)@+")
	ChanModes    string        // Raw CHANMODES string (e.g., "b,k,l,imnpst")
	ChanModesA   map[rune]bool // Type A modes (list modes, e.g. b)
	ChanModesB   map[rune]bool // Type B modes (always parameterized, e.g. k)
	ChanModesC   map[rune]bool // Type C modes (parameterized only when set, e.g. l)
	ChanModesD   map[rune]bool // Type D modes (boolean flags, e.g. imnpst)
	mu           sync.RWMutex  // Mutex for thread-safe access
}

// classification builds an immutable snapshot for the mode parser, combining the
// classified CHANMODES groups with the membership modes from PREFIX. When the server
// never advertised CHANMODES, RFC1459-style defaults are substituted so parsing still
// consumes parameters correctly. Callers should hold the client lock for reads.
func (sc *ServerCapabilities) classification() ModeClassification {
	a, b, c, d := sc.ChanModesA, sc.ChanModesB, sc.ChanModesC, sc.ChanModesD
	if len(a)+len(b)+len(c)+len(d) == 0 {
		a, b, c, d = classifyChanModes(defaultChanModes)
	}
	prefix := make(map[rune]bool, len(sc.Prefix))
	for _, modeLetter := range sc.Prefix {
		prefix[modeLetter] = true
	}
	return ModeClassification{List: a, Param: b, SetParam: c, Flag: d, Prefix: prefix}
}

// prefixForMode returns the display prefix char for a membership mode letter
// (e.g. 'o' -> '@'), and whether one exists. Callers should hold the client lock.
func (sc *ServerCapabilities) prefixForMode(modeLetter rune) (rune, bool) {
	for prefixChar, letter := range sc.Prefix {
		if letter == modeLetter {
			return prefixChar, true
		}
	}
	return 0, false
}

// prefixRank returns prefix chars ordered by descending privilege, derived from the
// order they appear in the PREFIX string (e.g. "(ov)@+" -> ['@','+']). Used to keep a
// user's stored prefix string sorted highest-first. Callers should hold the client lock.
func (sc *ServerCapabilities) prefixRank() []rune {
	if close := strings.IndexRune(sc.PrefixString, ')'); close >= 0 {
		return []rune(sc.PrefixString[close+1:])
	}
	return nil
}

// applyUserPrefix adds or removes the prefix char for the given membership mode letter
// in a user's stored prefix string, keeping it ordered highest-privilege first.
// Callers should hold the client lock.
func (sc *ServerCapabilities) applyUserPrefix(current string, modeLetter rune, add bool) string {
	prefixChar, ok := sc.prefixForMode(modeLetter)
	if !ok {
		return current
	}
	present := make(map[rune]bool)
	for _, r := range current {
		present[r] = true
	}
	if add {
		present[prefixChar] = true
	} else {
		delete(present, prefixChar)
	}

	var b strings.Builder
	for _, r := range sc.prefixRank() {
		if present[r] {
			b.WriteRune(r)
			delete(present, r)
		}
	}
	// Preserve any prefix chars not present in PREFIX rank (defensive for exotic servers).
	for r := range present {
		b.WriteRune(r)
	}
	return b.String()
}

// IsConnected returns whether the client is connected.
//
// This is a pure, side-effect-free read of the connection flag. Liveness
// detection (dead-connection / timeout handling) is owned entirely by the
// underlying ergochat/irc-go pingLoop, which PINGs on a KeepAlive interval and
// treats an unacked PING as fatal, firing the DisconnectCallback. That callback
// is the single authority that flips connected -> false. Keeping this method a
// plain getter avoids the old asymmetric latch where a passive staleness check
// could mark the connection dead while the live read loop kept delivering
// messages, leaving the UI permanently "disconnected" despite active traffic.
func (c *IRCClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// IsConnectedDirect is retained for its existing call sites and is now identical
// to IsConnected (both are pure reads of the connection flag).
func (c *IRCClient) IsConnectedDirect() bool {
	return c.IsConnected()
}

// NewIRCClient creates a new IRC client
func NewIRCClient(network *storage.Network, eventBus *events.EventBus, storage *storage.Storage) *IRCClient {
	var saslMechanism, saslUsername, saslPassword string
	if network.SASLMechanism != nil {
		saslMechanism = *network.SASLMechanism
	}
	if network.SASLUsername != nil {
		saslUsername = *network.SASLUsername
	}
	if network.SASLPassword != nil {
		saslPassword = *network.SASLPassword
	}

	client := &IRCClient{
		network:             network,
		eventBus:            eventBus,
		storage:             storage,
		saslEnabled:         network.SASLEnabled,
		saslMechanism:       saslMechanism,
		saslUsername:        saslUsername,
		saslPassword:        saslPassword,
		saslInProgress:      false,
		saslAuthenticated:   false,
		saslCapRequested:    false,
		saslCapAcknowledged: false,
		namesInProgress:     make(map[string]bool),
		whoisInProgress:     make(map[string]*WhoisInfo),
		enabledCaps:         make(map[string]bool),
		banLists:            make(map[string][]BanEntry),
		serverCapabilities: &ServerCapabilities{
			Prefix:       make(map[rune]rune),
			PrefixString: "",
			ChanModes:    "",
		},
		rateLimiter: NewRateLimiter(4, 2*time.Second),
	}

	// Debug: Log SASL configuration
	if network.SASLEnabled {
		logger.Log.Debug().
			Str("network", network.Name).
			Str("mechanism", saslMechanism).
			Str("username", saslUsername).
			Msg("SASL enabled")
	} else {
		logger.Log.Debug().Str("network", network.Name).Msg("SASL disabled")
	}

	// Create ircevent connection
	client.conn = &ircevent.Connection{
		Server:        fmt.Sprintf("%s:%d", network.Address, network.Port),
		Nick:          network.Nickname,
		User:          network.Username,
		RealName:      network.Realname,
		UseTLS:        network.TLS,
		Password:      network.Password,
		ReconnectFreq: 0, // Disable automatic reconnection - we'll handle it manually
		// Liveness detection is owned by the library's pingLoop: it sends a
		// keepalive PING every KeepAlive and treats an unacked PING (within
		// Timeout, enforced via socket read/write deadlines) as fatal, firing
		// the DisconnectCallback. This is the single source of truth for whether
		// the connection is alive, including across OS sleep/wake. Constraint:
		// KeepAlive must be >= Timeout.
		Timeout:   1 * time.Minute,
		KeepAlive: 3 * time.Minute,
	}

	// Set up event handlers
	client.setupHandlers()

	return client
}

// setupHandlers sets up IRC event handlers
func (c *IRCClient) setupHandlers() {
	// Add a raw message handler to see ALL incoming messages (for debugging)
	// Also track last message time for connection health checking
	c.conn.AddCallback("", func(e ircmsg.Message) {
		// Update last message time for any message from server (indicates connection is alive)
		c.mu.Lock()
		c.lastMessageTime = time.Now()
		c.mu.Unlock()

		// Only log CAP and AUTHENTICATE messages to avoid spam
		if len(e.Command) > 0 && (e.Command == "CAP" || e.Command == "AUTHENTICATE" || strings.HasPrefix(e.Command, "90")) {
			rawLine, _ := e.Line()
			logger.Log.Debug().Str("raw_line", rawLine).Msg("RAW IRC message")
		}
	})

	// Connection established
	c.conn.AddConnectCallback(func(e ircmsg.Message) {
		c.mu.Lock()
		c.connected = true
		c.lastMessageTime = time.Now()
		c.mu.Unlock()

		// Store connection message in status window
		rawLine, _ := e.Line()
		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil, // Status window
			User:        "*",
			Message:     "Connected to server",
			MessageType: "status",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		// Always start IRCv3 capability negotiation
		c.storage.WriteMessage(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     "Starting IRCv3 capability negotiation (CAP LS 302)",
			MessageType: "status",
			Timestamp:   time.Now(),
		})
		c.conn.SendRaw("CAP LS 302")

		// On connection established, restore channel state after restart
		// Clear all channel_users entries since we're not actually joined anymore
		if err := c.storage.ClearNetworkChannelUsers(c.networkID); err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to clear channel users on connection")
		} else {
			logger.Log.Debug().Msg("Cleared channel users on connection (restoring state after restart)")
		}

		c.eventBus.Emit(events.Event{
			Type:      EventConnectionEstablished,
			Data:      map[string]interface{}{"network": c.network.Address, "networkId": c.networkID},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Connection lost
	c.conn.AddDisconnectCallback(func(e ircmsg.Message) {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()

		// Write "Disconnected" messages to all open/joined channels BEFORE clearing users
		// This ensures we can still identify which channels the user was in
		// Do this SYNCHRONOUSLY so it completes before the process can exit
		logger.Log.Info().Int64("network_id", c.networkID).Msg("Disconnect callback: writing messages to channels")

		var channelsToNotify []storage.Channel
		if c.network.Nickname != "" {
			// Get channels that are open or where we're joined
			openChannels, err := c.storage.GetOpenChannels(c.networkID, c.network.Nickname)
			if err == nil && len(openChannels) > 0 {
				channelsToNotify = openChannels
				logger.Log.Debug().Int64("network_id", c.networkID).Int("open_channels", len(openChannels)).Msg("Disconnect callback: found open channels via GetOpenChannels")
			} else if err != nil {
				logger.Log.Debug().Err(err).Int64("network_id", c.networkID).Msg("Disconnect callback: error getting open channels")
			}
		}

		// Fallback: if GetOpenChannels didn't work or returned nothing, get all channels
		// and filter to those that are open
		if len(channelsToNotify) == 0 {
			allChannels, err := c.storage.GetChannels(c.networkID)
			if err == nil {
				for _, ch := range allChannels {
					if ch.IsOpen {
						channelsToNotify = append(channelsToNotify, ch)
					}
				}
				logger.Log.Debug().Int64("network_id", c.networkID).Int("open_channels", len(channelsToNotify)).Int("total_channels", len(allChannels)).Msg("Disconnect callback: found open channels via fallback")
			} else {
				logger.Log.Debug().Err(err).Int64("network_id", c.networkID).Msg("Disconnect callback: error getting all channels")
			}
		}

		// Write "Disconnected" message to each channel
		if len(channelsToNotify) > 0 {
			logger.Log.Info().Int64("network_id", c.networkID).Int("channel_count", len(channelsToNotify)).Msg("Disconnect callback: writing disconnect messages to channels")
			for _, channel := range channelsToNotify {
				disconnectMsg := storage.Message{
					NetworkID:   c.networkID,
					ChannelID:   &channel.ID,
					User:        "*",
					Message:     "Disconnected",
					MessageType: "status",
					Timestamp:   time.Now(),
					RawLine:     "",
				}
				// Use WriteMessageDirect to ensure immediate persistence
				if err := c.storage.WriteMessageDirect(disconnectMsg); err != nil {
					if err.Error() != "storage is closed" {
						logger.Log.Warn().Err(err).Int64("network_id", c.networkID).Str("channel", channel.Name).Msg("Disconnect callback: failed to write disconnect message to channel")
					} else {
						logger.Log.Debug().Int64("network_id", c.networkID).Str("channel", channel.Name).Msg("Disconnect callback: storage closed, skipping channel")
					}
				} else {
					logger.Log.Debug().Int64("network_id", c.networkID).Str("channel", channel.Name).Int64("channel_id", channel.ID).Msg("Disconnect callback: wrote disconnect message to channel")
				}
			}
		}

		// Clear all channel user lists for this network since we're disconnected
		// This prevents showing stale user lists when reconnecting
		channels, err := c.storage.GetChannels(c.networkID)
		if err == nil {
			for _, ch := range channels {
				if err := c.storage.ClearChannelUsers(ch.ID); err != nil {
					logger.Log.Error().Err(err).Str("channel", ch.Name).Msg("Failed to clear users for channel")
				} else {
					logger.Log.Debug().Str("channel", ch.Name).Msg("Cleared user list for channel")
				}
			}
		}

		// Store disconnection message in status window
		rawLine, _ := e.Line()
		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil, // Status window
			User:        "*",
			Message:     "Disconnected from server",
			MessageType: "status",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		c.eventBus.Emit(events.Event{
			Type:      EventConnectionLost,
			Data:      map[string]interface{}{"network": c.network.Address, "networkId": c.networkID},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		logger.Log.Debug().Int64("network_id", c.networkID).Msg("Disconnect callback: finished processing disconnect")
	})

	// PRIVMSG received
	c.conn.AddCallback("PRIVMSG", func(e ircmsg.Message) {
		channel := e.Params[0]
		message := e.Params[1]
		user := e.Nick()

		// Check if this is a CTCP message (wrapped in \001)
		if len(message) >= 2 && message[0] == '\001' && message[len(message)-1] == '\001' {
			ctcpMessage := message[1 : len(message)-1] // Remove \001 delimiters
			parts := strings.Fields(ctcpMessage)
			if len(parts) > 0 {
				ctcpCommand := strings.ToUpper(parts[0])
				ctcpArgs := ""
				if len(parts) > 1 {
					ctcpArgs = strings.Join(parts[1:], " ")
				}

				// Handle CTCP requests
				if ctcpCommand == "ACTION" {
					// CTCP ACTION - already handled, but store as action type
					// Determine if it's a channel or private message
					var channelID *int64
					var pmTarget string
					if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
						ch, err := c.storage.GetChannelByName(c.networkID, channel)
						if err == nil {
							channelID = &ch.ID
						}
					} else {
						// Private message - create or get PM conversation keyed by the peer
						pmTarget = c.pmPeer(user, channel)
						_, err := c.storage.GetOrCreatePMConversation(c.networkID, pmTarget, c.network.Nickname)
						if err != nil {
							logger.Log.Error().Err(err).Str("user", user).Str("pmTarget", pmTarget).Msg("Failed to create/get PM conversation")
						}
					}

					rawLine, _ := e.Line()
					msg := storage.Message{
						NetworkID:   c.networkID,
						ChannelID:   channelID,
						User:        user,
						Message:     fmt.Sprintf("* %s %s", user, ctcpArgs),
						MessageType: "action",
						Timestamp:   c.getMessageTime(e),
						RawLine:     rawLine,
						PMTarget:    pmTarget,
					}
					c.storage.WriteMessageSync(msg)
				} else {
					// Other CTCP requests - send response
					c.handleCTCPRequest(user, ctcpCommand, ctcpArgs)
					// Don't store CTCP requests as regular messages
					return
				}
			}
			return
		}

		// Regular PRIVMSG (not CTCP)
		// Handle echo-message: when the cap is enabled, the server echoes back our
		// sent messages as the canonical copy. We store them here and skip storage
		// in SendMessage. When echo-message is NOT enabled, drop self-to-self echoes.
		if strings.EqualFold(user, c.network.Nickname) {
			if !c.isEchoMessage(e) {
				// echo-message not enabled; this is a legacy self-echo - skip it
				if strings.EqualFold(channel, c.network.Nickname) {
					return
				}
			}
			// With echo-message enabled, fall through to store the server echo
		}

		// Determine if it's a channel or private message
		var channelID *int64
		var pmTarget string
		if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
			// Channel message - look up channel ID
			ch, err := c.storage.GetChannelByName(c.networkID, channel)
			if err == nil {
				channelID = &ch.ID
			} else {
				// Channel not found in database - might be a channel we haven't joined yet
				// Store with nil channel_id for now
				logger.Log.Debug().Err(err).Str("channel", channel).Msg("Channel not found in database")
			}
		} else {
			// Private message - create or get PM conversation keyed by the peer
			pmTarget = c.pmPeer(user, channel)
			_, err := c.storage.GetOrCreatePMConversation(c.networkID, pmTarget, c.network.Nickname)
			if err != nil {
				logger.Log.Error().Err(err).Str("user", user).Str("pmTarget", pmTarget).Msg("Failed to create/get PM conversation")
			}
		}

		rawLine, _ := e.Line()

		msg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   channelID,
			User:        user,
			Message:     message,
			MessageType: "privmsg",
			Timestamp:   c.getMessageTime(e),
			RawLine:     rawLine,
			PMTarget:    pmTarget,
		}

		// Store message (use sync write so it appears immediately)
		if err := c.storage.WriteMessageSync(msg); err != nil {
			c.eventBus.Emit(events.Event{
				Type:      EventError,
				Data:      map[string]interface{}{"error": err.Error()},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}

		// Emit event
		c.eventBus.Emit(events.Event{
			Type: EventMessageReceived,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"user":      user,
				"message":   message,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// User joined channel
	c.conn.AddCallback("JOIN", func(e ircmsg.Message) {
		channel := e.Params[0]
		user := e.Nick()
		logger.Log.Debug().
			Str("user", user).
			Str("channel", channel).
			Str("network", c.network.Name).
			Str("our_nick", c.network.Nickname).
			Msg("User joined channel")

		// Get or create channel in database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		channelCreated := false
		if err != nil {
			// Channel doesn't exist, create it
			logger.Log.Debug().Str("channel", channel).Msg("Channel doesn't exist, creating it")
			now := time.Now()
			newChannel := &storage.Channel{
				NetworkID: c.networkID,
				Name:      channel,
				AutoJoin:  false,
				IsOpen:    false, // Dialog not open yet - will be opened when user selects it
				CreatedAt: time.Now(),
				UpdatedAt: &now,
			}
			if err := c.storage.CreateChannel(newChannel); err != nil {
				logger.Log.Error().Err(err).Str("channel", channel).Msg("Failed to create channel")
				// Still try to continue with the rest
			} else {
				logger.Log.Debug().Str("channel", channel).Msg("Successfully created channel in database")
				ch = newChannel
				channelCreated = true
			}
		}

		// If the current user joined, mark channel as open (JOINED state)
		channelUpdated := false
		if strings.EqualFold(user, c.network.Nickname) && ch != nil {
			if !ch.IsOpen {
				c.storage.UpdateChannelIsOpen(ch.ID, true)
				channelUpdated = true
			}
		}

		// Add user to channel user list (for all users, not just ourselves)
		if ch != nil {
			if err := c.storage.AddChannelUser(ch.ID, user, ""); err != nil {
				logger.Log.Error().Err(err).Str("user", user).Str("channel", channel).Msg("Failed to add user to channel user list")
			} else {
				logger.Log.Debug().Str("user", user).Str("channel", channel).Msg("Added user to channel user list")
				// Verify the write is committed by reading it back (forces WAL sync)
				// This ensures the user is immediately visible when the frontend queries
				_, _ = c.storage.GetChannelUsers(ch.ID)
			}
		}

		// If the current user joined, request fresh NAMES list to update user list
		// Don't clear channel users - keep ourselves in the list so GetJoinedChannels works immediately
		if strings.EqualFold(user, c.network.Nickname) {
			logger.Log.Info().
				Str("channel", channel).
				Int64("network_id", c.networkID).
				Msg("Our user joining, requesting NAMES list to update user list")
			// Request NAMES to get full user list - this will update the channel_users table
			// We don't clear first because we want GetJoinedChannels to work immediately
			if err := c.conn.SendRaw(fmt.Sprintf("NAMES %s", channel)); err != nil {
				logger.Log.Error().Err(err).Str("channel", channel).Msg("Failed to send NAMES command")
			} else {
				logger.Log.Debug().Str("channel", channel).Msg("NAMES command sent successfully")
			}
		}

		// Store join message in the channel (use sync write so it appears immediately)
		if ch != nil {
			rawLine, _ := e.Line()
			joinMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   &ch.ID,
				User:        user,
				Message:     fmt.Sprintf("%s joined the channel", user),
				MessageType: "join",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			if err := c.storage.WriteMessageSync(joinMsg); err != nil {
				// During shutdown, storage may be closed - this is expected
				if err.Error() == "storage is closed" {
					logger.Log.Debug().Msg("Skipping join message storage (storage closed during shutdown)")
				} else {
					logger.Log.Error().Err(err).Msg("Failed to store join message")
				}
			}
		}

		// Emit event
		c.eventBus.Emit(events.Event{
			Type: EventUserJoined,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"user":      user,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		// Emit channels changed event if channel was created or updated
		if channelCreated || (channelUpdated && strings.EqualFold(user, c.network.Nickname)) {
			c.eventBus.Emit(events.Event{
				Type: EventChannelsChanged,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// User parted channel
	c.conn.AddCallback("PART", func(e ircmsg.Message) {
		channel := e.Params[0]
		user := e.Nick()
		reason := ""
		if len(e.Params) > 1 {
			reason = e.Params[1]
		}

		// Get channel and remove user from user list
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		channelUpdated := false
		if err == nil {
			if err := c.storage.RemoveChannelUser(ch.ID, user); err != nil {
				logger.Log.Error().Err(err).Str("user", user).Str("channel", channel).Msg("Failed to remove user from channel user list")
			} else {
				logger.Log.Debug().Str("user", user).Str("channel", channel).Msg("Removed user from channel user list")
				// Verify the write is committed by reading it back (forces WAL sync)
				// This ensures the user is immediately removed when the frontend queries
				_, _ = c.storage.GetChannelUsers(ch.ID)
			}

			// If the current user parted, mark channel as closed (CLOSED state)
			if strings.EqualFold(user, c.network.Nickname) {
				c.storage.UpdateChannelIsOpen(ch.ID, false)
				channelUpdated = true
			}

			// Store part message
			rawLine, _ := e.Line()
			partMsg := storage.Message{
				NetworkID: c.networkID,
				ChannelID: &ch.ID,
				User:      user,
				Message: fmt.Sprintf("%s left the channel%s", user, func() string {
					if reason != "" {
						return fmt.Sprintf(" (%s)", reason)
					}
					return ""
				}()),
				MessageType: "part",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			if err := c.storage.WriteMessageSync(partMsg); err != nil {
				// During shutdown, storage may be closed - this is expected
				if err.Error() == "storage is closed" {
					logger.Log.Debug().Msg("Skipping part message storage (storage closed during shutdown)")
				} else {
					logger.Log.Error().Err(err).Msg("Failed to store part message")
				}
			}
		}

		c.eventBus.Emit(events.Event{
			Type: EventUserParted,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"user":      user,
				"reason":    reason,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		// Emit channels changed event if our own part updated the channel state
		if channelUpdated {
			c.eventBus.Emit(events.Event{
				Type: EventChannelsChanged,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// User quit
	c.conn.AddCallback("QUIT", func(e ircmsg.Message) {
		user := e.Nick()
		reason := ""
		if len(e.Params) > 0 {
			reason = e.Params[0]
		}

		// Remove user from all channels they were in
		channels, err := c.storage.GetChannels(c.networkID)
		if err == nil {
			for _, ch := range channels {
				// Check if user is in this channel
				users, err := c.storage.GetChannelUsers(ch.ID)
				if err == nil {
					for _, u := range users {
						if u.Nickname == user {
							// User is in this channel, remove them
							if err := c.storage.RemoveChannelUser(ch.ID, user); err != nil {
								logger.Log.Error().Err(err).Str("user", user).Str("channel", ch.Name).Msg("Failed to remove user from channel user list")
							} else {
								logger.Log.Debug().Str("user", user).Str("channel", ch.Name).Msg("Removed user from channel user list")
								// Verify the write is committed by reading it back (forces WAL sync)
								// This ensures the user is immediately removed when the frontend queries
								_, _ = c.storage.GetChannelUsers(ch.ID)
							}

							// Store quit message in the channel
							rawLine, _ := e.Line()
							quitMsg := storage.Message{
								NetworkID: c.networkID,
								ChannelID: &ch.ID,
								User:      user,
								Message: fmt.Sprintf("%s quit%s", user, func() string {
									if reason != "" {
										return fmt.Sprintf(" (%s)", reason)
									}
									return ""
								}()),
								MessageType: "quit",
								Timestamp:   time.Now(),
								RawLine:     rawLine,
							}
							if err := c.storage.WriteMessageSync(quitMsg); err != nil {
								// During shutdown, storage may be closed - this is expected
								if err.Error() == "storage is closed" {
									logger.Log.Debug().Msg("Skipping quit message storage (storage closed during shutdown)")
								} else {
									logger.Log.Error().Err(err).Msg("Failed to store quit message")
								}
							}
							break
						}
					}
				}
			}
		}

		c.eventBus.Emit(events.Event{
			Type: EventUserQuit,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"user":      user,
				"reason":    reason,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// User kicked from channel
	c.conn.AddCallback("KICK", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[0]
		kickedUser := e.Params[1]
		kicker := e.Nick()
		reason := ""
		if len(e.Params) > 2 {
			reason = e.Params[2]
		}

		// Get channel from database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		channelUpdated := false
		if err == nil {
			// Remove kicked user from channel user list
			if err := c.storage.RemoveChannelUser(ch.ID, kickedUser); err != nil {
				logger.Log.Error().Err(err).Str("user", kickedUser).Str("channel", channel).Msg("Failed to remove kicked user from channel user list")
			} else {
				logger.Log.Debug().Str("user", kickedUser).Str("channel", channel).Msg("Removed kicked user from channel user list")
				// Verify the write is committed by reading it back (forces WAL sync)
				// This ensures the user is immediately removed when the frontend queries
				_, _ = c.storage.GetChannelUsers(ch.ID)
			}

			// If we were kicked, mark channel as closed
			if strings.EqualFold(kickedUser, c.network.Nickname) {
				c.storage.UpdateChannelIsOpen(ch.ID, false)
				channelUpdated = true
			}

			// Store kick message in the channel (use sync write so it appears immediately)
			rawLine, _ := e.Line()
			kickMsg := storage.Message{
				NetworkID: c.networkID,
				ChannelID: &ch.ID,
				User:      kicker,
				Message: fmt.Sprintf("%s kicked %s%s", kicker, kickedUser, func() string {
					if reason != "" {
						return fmt.Sprintf(" (%s)", reason)
					}
					return ""
				}()),
				MessageType: "kick",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			if err := c.storage.WriteMessageSync(kickMsg); err != nil {
				// During shutdown, storage may be closed - this is expected
				if err.Error() == "storage is closed" {
					logger.Log.Debug().Msg("Skipping kick message storage (storage closed during shutdown)")
				} else {
					logger.Log.Error().Err(err).Msg("Failed to store kick message")
				}
			}
		}

		// Emit event
		c.eventBus.Emit(events.Event{
			Type: EventUserKicked,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"kicker":    kicker,
				"user":      kickedUser,
				"reason":    reason,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		// Emit channels changed event if we were kicked
		if channelUpdated {
			c.eventBus.Emit(events.Event{
				Type: EventChannelsChanged,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// Nick change
	c.conn.AddCallback("NICK", func(e ircmsg.Message) {
		oldNick := e.Nick()
		newNick := e.Params[0]

		// Update nickname in all channels for this network
		if err := c.storage.UpdateChannelUserNickname(c.networkID, oldNick, newNick); err != nil {
			logger.Log.Error().Err(err).Str("oldNick", oldNick).Str("newNick", newNick).Msg("Failed to update nickname in channel user lists")
		} else {
			logger.Log.Debug().Str("oldNick", oldNick).Str("newNick", newNick).Msg("Updated nickname in channel user lists")
		}

		c.eventBus.Emit(events.Event{
			Type: EventUserNick,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"oldNick":   oldNick,
				"newNick":   newNick,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Channel topic (RPL_TOPIC = 332) - received when topic is retrieved
	c.conn.AddCallback("332", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channel := e.Params[1]
		topic := e.Params[2]

		// Store topic in database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err == nil {
			c.storage.UpdateChannelTopic(ch.ID, topic)
		}

		c.eventBus.Emit(events.Event{
			Type: EventChannelTopic,
			Data: map[string]interface{}{
				"network": c.network.Address,
				"channel": channel,
				"topic":   topic,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// TOPIC command - when someone changes the topic
	c.conn.AddCallback("TOPIC", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[0]
		topic := ""
		if len(e.Params) > 1 {
			topic = e.Params[1]
		}

		// Store topic in database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err == nil {
			c.storage.UpdateChannelTopic(ch.ID, topic)
		}

		c.eventBus.Emit(events.Event{
			Type: EventChannelTopic,
			Data: map[string]interface{}{
				"network": c.network.Address,
				"channel": channel,
				"topic":   topic,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Track NAMES responses for building user list
	// RPL_NAMREPLY (353) - list of users in channel
	c.conn.AddCallback("353", func(e ircmsg.Message) {
		if len(e.Params) < 4 {
			return
		}
		channel := e.Params[2]
		namesList := e.Params[3]

		// Get channel from database (case-insensitive lookup)
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err != nil {
			logger.Log.Warn().Err(err).Str("channel", channel).Int64("network_id", c.networkID).Msg("Failed to find channel for NAMES response")
			return
		}

		// Use lowercase channel name as key for namesInProgress to handle case variations
		channelKey := strings.ToLower(channel)

		// On first NAMES response for a channel, clear the existing user list
		// This ensures we start fresh when receiving the NAMES list
		c.namesMu.Lock()
		if !c.namesInProgress[channelKey] {
			c.namesInProgress[channelKey] = true
			c.namesMu.Unlock()
			if err := c.storage.ClearChannelUsers(ch.ID); err != nil {
				logger.Log.Error().Err(err).Str("channel", channel).Msg("Failed to clear users for channel")
			} else {
				logger.Log.Debug().Str("channel", channel).Int("names_count", len(strings.Fields(namesList))).Msg("Cleared user list for channel before receiving NAMES")
			}
		} else {
			c.namesMu.Unlock()
		}

		// Parse names (format: "@nick1 +nick2 nick3")
		names := strings.Fields(namesList)
		addedCount := 0
		for _, nameWithMode := range names {
			// Extract mode prefix (@, +, etc.) and nickname
			nickname := nameWithMode
			modes := ""
			if len(nameWithMode) > 0 {
				// IRC prefixes: @ = op, + = voice, % = halfop, & = admin, ~ = owner
				for len(nickname) > 0 && (nickname[0] == '@' || nickname[0] == '+' || nickname[0] == '%' || nickname[0] == '&' || nickname[0] == '~') {
					modes += string(nickname[0])
					nickname = nickname[1:]
				}
			}
			if len(nickname) > 0 {
				if err := c.storage.AddChannelUser(ch.ID, nickname, modes); err != nil {
					logger.Log.Warn().Err(err).Str("nickname", nickname).Str("channel", channel).Msg("Failed to add user from NAMES")
				} else {
					addedCount++
				}
			}
		}
		logger.Log.Debug().Str("channel", channel).Int("added", addedCount).Int("total_in_response", len(names)).Msg("Processed NAMES response")
	})

	// RPL_ENDOFNAMES (366) - end of NAMES list
	c.conn.AddCallback("366", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[1]
		channelKey := strings.ToLower(channel)
		logger.Log.Debug().Str("channel", channel).Msg("Finished receiving user list for channel")

		// Mark that we're done receiving NAMES for this channel
		c.namesMu.Lock()
		delete(c.namesInProgress, channelKey)
		c.namesMu.Unlock()

		// Get the list of users in the channel to include in the event
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		var users []string
		if err == nil {
			channelUsers, err := c.storage.GetChannelUsers(ch.ID)
			if err == nil {
				users = make([]string, len(channelUsers))
				for i, cu := range channelUsers {
					users[i] = cu.Nickname
				}
			}
		}

		// Emit event when NAMES list is complete so frontend can refresh user list
		// Include user list so plugins can process all users at once
		logger.Log.Info().
			Str("channel", channel).
			Int("user_count", len(users)).
			Int64("network_id", c.networkID).
			Msg("NAMES list complete, emitting channel.names.complete event")
		c.eventBus.Emit(events.Event{
			Type: "channel.names.complete",
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"users":     users,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Handle IRC error numerics (400-599 range)
	// Common errors: 482 ERR_CHANOPRIVSNEEDED, 442 ERR_NOTONCHANNEL, 481 ERR_NOPRIVILEGES, etc.
	c.conn.AddCallback("482", func(e ircmsg.Message) {
		// ERR_CHANOPRIVSNEEDED - You're not a channel operator
		// Format: :server 482 nick #channel :You're not a channel operator
		errorMsg := "You're not a channel operator"
		var channel string
		if len(e.Params) >= 2 {
			channel = e.Params[1] // Channel is typically the second param
			if len(e.Params) >= 3 {
				errorMsg = e.Params[2] // Error message is usually the last param
			}
		}

		rawLine, _ := e.Line()

		// Try to store error in the channel if it's a channel-related error
		var channelID *int64
		if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
			ch, err := c.storage.GetChannelByName(c.networkID, channel)
			if err == nil {
				channelID = &ch.ID
			}
		}

		// Store error in channel (if found) or status window
		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   channelID, // Channel if found, otherwise status window
			User:        "*",
			Message:     fmt.Sprintf("Error: %s", errorMsg),
			MessageType: "error",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		// Emit error event with channel info
		c.eventBus.Emit(events.Event{
			Type: EventError,
			Data: map[string]interface{}{
				"network": c.network.Address,
				"channel": channel,
				"error":   errorMsg,
				"code":    "482",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// 442 ERR_NOTONCHANNEL
	c.conn.AddCallback("442", func(e ircmsg.Message) {
		// Format: :server 442 nick #channel :You're not on that channel
		errorMsg := "You're not on that channel"
		var channel string
		if len(e.Params) >= 2 {
			channel = e.Params[1]
			if len(e.Params) >= 3 {
				errorMsg = e.Params[2]
			}
		}

		rawLine, _ := e.Line()

		// Try to store error in the channel if it's a channel-related error
		var channelID *int64
		if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
			ch, err := c.storage.GetChannelByName(c.networkID, channel)
			if err == nil {
				channelID = &ch.ID
			}
		}

		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   channelID,
			User:        "*",
			Message:     fmt.Sprintf("Error: %s", errorMsg),
			MessageType: "error",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		c.eventBus.Emit(events.Event{
			Type: EventError,
			Data: map[string]interface{}{
				"network": c.network.Address,
				"channel": channel,
				"error":   errorMsg,
				"code":    "442",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// 481 ERR_NOPRIVILEGES
	c.conn.AddCallback("481", func(e ircmsg.Message) {
		// Format: :server 481 nick :Permission denied
		errorMsg := "Permission denied"
		if len(e.Params) >= 2 {
			errorMsg = e.Params[len(e.Params)-1]
		}

		rawLine, _ := e.Line()
		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil, // This error is usually not channel-specific
			User:        "*",
			Message:     fmt.Sprintf("Error: %s", errorMsg),
			MessageType: "error",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		c.eventBus.Emit(events.Event{
			Type: EventError,
			Data: map[string]interface{}{
				"network": c.network.Address,
				"error":   errorMsg,
				"code":    "481",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Channel MODE changes
	c.conn.AddCallback("MODE", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		target := e.Params[0]
		// Only handle channel modes (target starts with # or &); ignore user modes.
		if len(target) == 0 || (target[0] != '#' && target[0] != '&') {
			return
		}

		ch, err := c.storage.GetChannelByName(c.networkID, target)
		if err != nil {
			return
		}

		// Snapshot the server's mode grammar once under lock, then parse lock-free.
		c.mu.RLock()
		cls := c.serverCapabilities.classification()
		c.mu.RUnlock()

		changes := ParseModeChanges(e.Params[1], e.Params[2:], cls)

		// Surface the change in the channel itself (like join/part/kick), faithfully
		// mirroring what the server applied: "<actor> sets mode: +o-v+k a b key".
		// Bare list-mode *queries* (e.g. ban-list fetches) never reach here — the
		// server answers those with 367/368, not a MODE echo — so this won't fire for them.
		if len(changes) > 0 {
			actor := e.Nick()
			if actor == "" {
				actor = e.Source
			}
			rawLine, _ := e.Line()
			c.storage.WriteMessageSync(storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   &ch.ID,
				User:        actor,
				Message:     fmt.Sprintf("%s sets mode: %s", actor, strings.Join(e.Params[1:], " ")),
				MessageType: "mode",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			})
		}

		// Fold channel-level changes (D flags + B/C params) into the canonical mode
		// string, persisting only when it actually changed.
		newModes := applyChannelModes(ch.Modes, changes, cls)
		if newModes != ch.Modes {
			if err := c.storage.UpdateChannelModes(ch.ID, newModes); err != nil {
				logger.Log.Warn().Err(err).Str("channel", target).Msg("Failed to persist channel modes")
			}
		}
		// Always announce the mode change so the UI reloads the channel — both the modes
		// header and the "sets mode" line written above. This fires even for list-mode
		// changes (e.g. +b) where the canonical string is unchanged, which would otherwise
		// leave the new line invisible until the next poll.
		c.eventBus.Emit(events.Event{
			Type: EventChannelMode,
			Data: map[string]interface{}{
				"network": c.network.Address,
				"channel": target,
				"modes":   newModes,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		// Apply per-user membership (prefix) changes individually, emitting a granular
		// event per affected user so the member list re-groups live.
		for _, mc := range changes {
			if mc.Kind != ModeKindPrefix || mc.Param == "" {
				continue
			}
			current, gerr := c.storage.GetChannelUserModes(ch.ID, mc.Param)
			if gerr != nil {
				// User not tracked yet (e.g. NAMES not complete) — skip.
				continue
			}
			c.mu.RLock()
			updated := c.serverCapabilities.applyUserPrefix(current, mc.Mode, mc.Add)
			c.mu.RUnlock()
			if updated == current {
				continue
			}
			if err := c.storage.AddChannelUser(ch.ID, mc.Param, updated); err != nil {
				logger.Log.Warn().Err(err).Str("nick", mc.Param).Str("channel", target).Msg("Failed to update user modes")
				continue
			}
			added, removed := "", ""
			if mc.Add {
				added = string(mc.Mode)
			} else {
				removed = string(mc.Mode)
			}
			c.eventBus.Emit(events.Event{
				Type: EventChannelUserMode,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
					"channel":   target,
					"nick":      mc.Param,
					"modes":     updated,
					"added":     added,
					"removed":   removed,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// RPL_CHANNELMODEIS (324) - authoritative channel modes, typically on join or
	// in response to a "MODE #channel" query. Replaces the stored canonical string.
	c.conn.AddCallback("324", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channel := e.Params[1]
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err != nil {
			return
		}
		c.mu.RLock()
		cls := c.serverCapabilities.classification()
		c.mu.RUnlock()

		changes := ParseModeChanges(e.Params[2], e.Params[3:], cls)
		newModes := applyChannelModes("", changes, cls) // authoritative: rebuild from scratch
		if err := c.storage.UpdateChannelModes(ch.ID, newModes); err != nil {
			logger.Log.Warn().Err(err).Str("channel", channel).Msg("Failed to persist channel modes from 324")
			return
		}
		c.eventBus.Emit(events.Event{
			Type: EventChannelMode,
			Data: map[string]interface{}{
				"network": c.network.Address,
				"channel": channel,
				"modes":   newModes,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// RPL_BANLIST (367) - a single ban entry: <channel> <mask> [<setter> <time>]
	c.conn.AddCallback("367", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channel := e.Params[1]
		entry := BanEntry{Mask: e.Params[2]}
		if len(e.Params) >= 4 {
			entry.By = e.Params[3]
		}
		if len(e.Params) >= 5 {
			fmt.Sscanf(e.Params[4], "%d", &entry.Time)
		}
		key := strings.ToLower(channel)
		c.banListsMu.Lock()
		c.banLists[key] = append(c.banLists[key], entry)
		c.banListsMu.Unlock()
	})

	// RPL_ENDOFBANLIST (368) - end of ban list; flush collected entries to the frontend.
	c.conn.AddCallback("368", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[1]
		key := strings.ToLower(channel)
		c.banListsMu.Lock()
		entries := c.banLists[key]
		delete(c.banLists, key)
		c.banListsMu.Unlock()

		bans := make([]interface{}, len(entries))
		for i, b := range entries {
			bans[i] = map[string]interface{}{"mask": b.Mask, "by": b.By, "time": b.Time}
		}
		c.eventBus.Emit(events.Event{
			Type: EventChannelBanList,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"bans":      bans,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// MOTD (Message of the Day) - store in status window
	c.conn.AddCallback("372", func(e ircmsg.Message) {
		// RPL_MOTD line
		if len(e.Params) >= 2 {
			motdLine := e.Params[1]
			rawLine, _ := e.Line()
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil, // Status window
				User:        "*",
				Message:     motdLine,
				MessageType: "motd",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			c.storage.WriteMessage(statusMsg)
		}
	})

	// NOTICE messages - store in status window if not from a channel
	// Also handle CTCP responses (CTCP replies come as NOTICE)
	c.conn.AddCallback("NOTICE", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		target := e.Params[0]
		notice := e.Params[1]
		user := e.Nick()

		// Check if this is a CTCP response (wrapped in \001)
		if len(notice) >= 2 && notice[0] == '\001' && notice[len(notice)-1] == '\001' {
			ctcpMessage := notice[1 : len(notice)-1] // Remove \001 delimiters
			parts := strings.Fields(ctcpMessage)
			if len(parts) > 0 {
				ctcpCommand := strings.ToUpper(parts[0])
				ctcpResponse := ""
				if len(parts) > 1 {
					ctcpResponse = strings.Join(parts[1:], " ")
				}

				// Store CTCP response in status window
				rawLine, _ := e.Line()
				statusMsg := storage.Message{
					NetworkID:   c.networkID,
					ChannelID:   nil, // Status window
					User:        user,
					Message:     fmt.Sprintf("CTCP %s reply from %s: %s", ctcpCommand, user, ctcpResponse),
					MessageType: "ctcp",
					Timestamp:   c.getMessageTime(e),
					RawLine:     rawLine,
				}
				c.storage.WriteMessage(statusMsg)
				return
			}
		}

		// Regular NOTICE.
		// Channel-targeted notices (e.g. bot/announcement notices) belong in that
		// channel's buffer, mirroring how channel PRIVMSGs are routed.
		if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
			rawLine, _ := e.Line()
			var channelID *int64
			if ch, err := c.storage.GetChannelByName(c.networkID, target); err == nil {
				channelID = &ch.ID
			} else {
				logger.Log.Debug().Err(err).Str("channel", target).Msg("Channel not found for notice")
			}
			c.storage.WriteMessageSync(storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   channelID,
				User:        user,
				Message:     notice,
				MessageType: "notice",
				Timestamp:   c.getMessageTime(e),
				RawLine:     rawLine,
			})
			c.eventBus.Emit(events.Event{
				Type: EventMessageReceived,
				Data: map[string]interface{}{
					"network":     c.network.Address,
					"networkId":   c.networkID,
					"channel":     target,
					"user":        user,
					"message":     notice,
					"messageType": "notice",
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
			return
		}

		// Non-channel NOTICE.
		// Notices from a user/service (ChanServ, NickServ, another nick) carry a
		// "nick!user@host" prefix and route to that sender's query pane, keyed by
		// pm_target exactly like a PRIVMSG PM. Notices with a bare server-name
		// prefix (or addressed to "*") are genuine server notices and stay in the
		// Status window.
		if target == c.network.Nickname || (len(target) > 0 && target[0] != '#' && target[0] != '&') {
			rawLine, _ := e.Line()
			pmTarget := c.noticePMTarget(e.Source, user, target)

			if pmTarget != "" {
				// Open/refresh the query conversation so the pane appears in the sidebar.
				if _, err := c.storage.GetOrCreatePMConversation(c.networkID, pmTarget, c.network.Nickname); err != nil {
					logger.Log.Error().Err(err).Str("user", user).Str("pmTarget", pmTarget).Msg("Failed to create/get PM conversation for notice")
				}
			}

			msg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil, // PM rows and status rows both have a nil channel
				User:        user,
				Message:     notice,
				MessageType: "notice",
				Timestamp:   c.getMessageTime(e),
				RawLine:     rawLine,
				PMTarget:    pmTarget, // "" keeps it in Status; non-empty routes to a query pane
			}

			if pmTarget == "" {
				// Server notice — buffered write into Status, no event (unchanged).
				c.storage.WriteMessage(msg)
			} else {
				// Service/user notice — sync write so it shows immediately, plus a
				// message event so the query pane badges and updates live. The
				// messageType marks it a notice so chatty services don't trigger a
				// desktop notification on every line.
				c.storage.WriteMessageSync(msg)
				c.eventBus.Emit(events.Event{
					Type: EventMessageReceived,
					Data: map[string]interface{}{
						"network":     c.network.Address,
						"networkId":   c.networkID,
						"channel":     target,
						"user":        user,
						"message":     notice,
						"messageType": "notice",
					},
					Timestamp: time.Now(),
					Source:    events.EventSourceIRC,
				})
			}
		}
	})

	// Numeric replies (like RPL_WELCOME, etc.) - store important ones in status
	c.conn.AddCallback("001", func(e ircmsg.Message) {
		// RPL_WELCOME
		if len(e.Params) >= 1 {
			welcomeMsg := e.Params[len(e.Params)-1]
			rawLine, _ := e.Line()
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil, // Status window
				User:        "*",
				Message:     welcomeMsg,
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			c.storage.WriteMessage(statusMsg)
		}
	})

	// WHOIS response handlers
	// RPL_WHOISUSER (311) - Basic user information
	c.conn.AddCallback("311", func(e ircmsg.Message) {
		if len(e.Params) < 4 {
			return
		}
		nickname := e.Params[1]
		username := e.Params[2]
		hostmask := e.Params[3]
		realName := ""
		if len(e.Params) >= 5 {
			realName = e.Params[4]
		}

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].Username = username
		c.whoisInProgress[nickname].Hostmask = hostmask
		c.whoisInProgress[nickname].RealName = realName
		c.whoisMu.Unlock()
	})

	// RPL_WHOISSERVER (312) - Server information
	c.conn.AddCallback("312", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		server := e.Params[2]
		serverInfo := ""
		if len(e.Params) >= 4 {
			serverInfo = e.Params[3]
		}

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].Server = server
		c.whoisInProgress[nickname].ServerInfo = serverInfo
		c.whoisMu.Unlock()
	})

	// RPL_WHOISOPERATOR (313) - Operator status
	c.conn.AddCallback("313", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		nickname := e.Params[1]
		// User is an IRC operator - we can store this in a future field if needed
		logger.Log.Debug().Str("nickname", nickname).Msg("User is an IRC operator")
	})

	// RPL_WHOISIDLE (317) - Idle time and sign-on time
	c.conn.AddCallback("317", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		idleSeconds := int64(0)
		signOnTime := int64(0)

		// Parse idle time (seconds)
		if len(e.Params) >= 3 {
			fmt.Sscanf(e.Params[2], "%d", &idleSeconds)
		}
		// Parse sign-on time (unix timestamp)
		if len(e.Params) >= 4 {
			fmt.Sscanf(e.Params[3], "%d", &signOnTime)
		}

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].IdleTime = idleSeconds
		c.whoisInProgress[nickname].SignOnTime = signOnTime
		c.whoisMu.Unlock()
	})

	// RPL_ENDOFWHOIS (318) - End of WHOIS
	c.conn.AddCallback("318", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		nickname := e.Params[1]

		c.whoisMu.Lock()
		whoisInfo := c.whoisInProgress[nickname]
		delete(c.whoisInProgress, nickname)
		c.whoisMu.Unlock()

		if whoisInfo != nil {
			// Emit WHOIS event with complete information
			c.eventBus.Emit(events.Event{
				Type: EventWhoisReceived,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
					"whois":     whoisInfo,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// RPL_WHOISCHANNELS (319) - Channels user is in
	c.conn.AddCallback("319", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		channelsStr := e.Params[2]

		// Parse channels (space-separated list)
		channels := strings.Fields(channelsStr)

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].Channels = channels
		c.whoisMu.Unlock()
	})

	// RPL_WHOISACCOUNT (330) - Account name (if logged in)
	c.conn.AddCallback("330", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		accountName := e.Params[2]

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].AccountName = accountName
		c.whoisMu.Unlock()
	})

	// ISUPPORT (005) - Server capabilities
	c.conn.AddCallback("005", func(e ircmsg.Message) {
		// RPL_ISUPPORT - Server capability parameters
		// Format: :server 005 nickname PREFIX=(ov)@+ CHANMODES=b,k,l,imnpst ... :are supported by this server
		if len(e.Params) < 2 {
			return
		}

		// Parse all parameters (skip first param which is our nickname)
		for i := 1; i < len(e.Params); i++ {
			param := e.Params[i]
			if param == "" || param[0] == ':' {
				// Last parameter starts with ':' and is usually a description
				break
			}

			// Parse PREFIX parameter: PREFIX=(ov)@+
			if strings.HasPrefix(param, "PREFIX=") {
				prefixValue := param[7:] // Skip "PREFIX="
				c.parsePREFIX(prefixValue)
			}

			// Parse CHANMODES parameter: CHANMODES=b,k,l,imnpst
			if strings.HasPrefix(param, "CHANMODES=") {
				chanModesValue := param[10:] // Skip "CHANMODES="
				a, b, cc, d := classifyChanModes(chanModesValue)
				c.mu.Lock()
				c.serverCapabilities.ChanModes = chanModesValue
				c.serverCapabilities.ChanModesA = a
				c.serverCapabilities.ChanModesB = b
				c.serverCapabilities.ChanModesC = cc
				c.serverCapabilities.ChanModesD = d
				c.mu.Unlock()
				logger.Log.Debug().
					Str("chanmodes", chanModesValue).
					Str("network", c.network.Name).
					Msg("Parsed CHANMODES from ISUPPORT")
			}
		}
	})

	// RPL_LISTSTART (321) - Beginning of LIST response
	c.conn.AddCallback("321", func(e ircmsg.Message) {
		c.channelListMu.Lock()
		c.channelListItems = nil // Reset list
		c.channelListMu.Unlock()
		logger.Log.Debug().Int64("network_id", c.networkID).Msg("Channel LIST started")
	})

	// RPL_LIST (322) - Channel list entry: <channel> <visible> :<topic>
	c.conn.AddCallback("322", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channelName := e.Params[1]
		usersCount := 0
		fmt.Sscanf(e.Params[2], "%d", &usersCount)
		topic := ""
		if len(e.Params) >= 4 {
			topic = e.Params[3]
		}

		item := ChannelListItem{
			Channel:   channelName,
			Users:     usersCount,
			Topic:     topic,
			NetworkID: c.networkID,
		}

		c.channelListMu.Lock()
		c.channelListItems = append(c.channelListItems, item)
		c.channelListMu.Unlock()
	})

	// RPL_LISTEND (323) - End of LIST response
	c.conn.AddCallback("323", func(e ircmsg.Message) {
		c.channelListMu.Lock()
		items := make([]ChannelListItem, len(c.channelListItems))
		copy(items, c.channelListItems)
		c.channelListItems = nil
		c.channelListMu.Unlock()

		logger.Log.Debug().Int64("network_id", c.networkID).Int("count", len(items)).Msg("Channel LIST completed")

		// Convert items to interface slice for event data
		itemsData := make([]interface{}, len(items))
		for i, item := range items {
			itemsData[i] = map[string]interface{}{
				"channel":   item.Channel,
				"users":     item.Users,
				"topic":     item.Topic,
				"networkId": item.NetworkID,
			}
		}

		c.eventBus.Emit(events.Event{
			Type: EventChannelListEnd,
			Data: map[string]interface{}{
				"networkId": c.networkID,
				"channels":  itemsData,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// IRCv3 capability negotiation (always enabled, not just for SASL)
	var capLSBuffer strings.Builder

	c.conn.AddCallback("CAP", func(e ircmsg.Message) {
		rawLine, _ := e.Line()
		c.storage.WriteMessage(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("CAP received: %s", rawLine),
			MessageType: "status",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		})

		if len(e.Params) < 2 {
			return
		}
		subcommand := e.Params[1]

		switch subcommand {
		case "LS":
			if len(e.Params) >= 3 {
				capabilities := e.Params[2]
				isContinuation := len(e.Params) > 3 && e.Params[2] == "*"

				if isContinuation {
					capLSBuffer.WriteString(capabilities)
					capLSBuffer.WriteString(" ")
				} else {
					capLSBuffer.WriteString(capabilities)
					allCaps := capLSBuffer.String()
					capLSBuffer.Reset()

					c.storage.WriteMessage(storage.Message{
						NetworkID:   c.networkID,
						ChannelID:   nil,
						User:        "*",
						Message:     fmt.Sprintf("CAP LS response: %s", allCaps),
						MessageType: "status",
						Timestamp:   time.Now(),
					})

					// Build list of caps to request: only those both wanted and offered
					var toRequest []string
					for _, wantedCap := range requestedCaps {
						if wantedCap == "sasl" && !c.saslEnabled {
							continue
						}
						if contains(allCaps, wantedCap) {
							toRequest = append(toRequest, wantedCap)
						}
					}

					if len(toRequest) > 0 {
						reqStr := strings.Join(toRequest, " ")
						c.storage.WriteMessage(storage.Message{
							NetworkID:   c.networkID,
							ChannelID:   nil,
							User:        "*",
							Message:     fmt.Sprintf("Requesting capabilities: %s", reqStr),
							MessageType: "status",
							Timestamp:   time.Now(),
						})
						c.conn.SendRaw("CAP REQ :" + reqStr)
						if c.saslEnabled && contains(allCaps, "sasl") {
							c.saslCapRequested = true
						}
					} else {
						c.storage.WriteMessage(storage.Message{
							NetworkID:   c.networkID,
							ChannelID:   nil,
							User:        "*",
							Message:     "No requested capabilities supported, ending CAP negotiation",
							MessageType: "status",
							Timestamp:   time.Now(),
						})
						c.conn.SendRaw("CAP END")
					}
				}
			}
		case "ACK":
			if len(e.Params) >= 3 {
				acked := e.Params[2]
				c.mu.Lock()
				for _, ackedCap := range strings.Fields(acked) {
					c.enabledCaps[ackedCap] = true
				}
				c.mu.Unlock()

				c.storage.WriteMessage(storage.Message{
					NetworkID:   c.networkID,
					ChannelID:   nil,
					User:        "*",
					Message:     fmt.Sprintf("Capabilities enabled: %s", acked),
					MessageType: "status",
					Timestamp:   time.Now(),
				})

				if contains(acked, "sasl") && c.saslEnabled {
					c.saslCapAcknowledged = true
					c.startSASLAuth()
				} else {
					c.conn.SendRaw("CAP END")
				}
			}
		case "NAK":
			c.storage.WriteMessage(storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     "Capability request rejected (NAK), ending CAP negotiation",
				MessageType: "status",
				Timestamp:   time.Now(),
			})
			c.conn.SendRaw("CAP END")
		}
	})

	// SASL-specific handlers (always registered; no-op if SASL isn't in use)
	c.conn.AddCallback("AUTHENTICATE", func(e ircmsg.Message) {
		if !c.saslEnabled {
			return
		}
		c.handleAUTHENTICATE(e)
	})

	c.conn.AddCallback("900", func(e ircmsg.Message) {
		if !c.saslEnabled {
			return
		}
		c.mu.Lock()
		c.saslAuthenticated = true
		c.saslInProgress = false
		c.mu.Unlock()
		c.storage.WriteMessage(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     "SASL authentication successful",
			MessageType: "status",
			Timestamp:   time.Now(),
		})
		c.conn.SendRaw("CAP END")
		c.eventBus.Emit(events.Event{
			Type:      EventSASLSuccess,
			Data:      map[string]interface{}{"network": c.network.Address},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	c.conn.AddCallback("901", func(e ircmsg.Message) {
		if !c.saslEnabled {
			return
		}
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
		c.storage.WriteMessage(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     "SASL authentication failed",
			MessageType: "status",
			Timestamp:   time.Now(),
		})
		c.conn.SendRaw("CAP END")
		c.eventBus.Emit(events.Event{
			Type:      EventSASLFailed,
			Data:      map[string]interface{}{"network": c.network.Address, "error": "Authentication failed"},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	c.conn.AddCallback("904", func(e ircmsg.Message) {
		if !c.saslEnabled {
			return
		}
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
		c.storage.WriteMessage(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     "SASL authentication failed",
			MessageType: "status",
			Timestamp:   time.Now(),
		})
		c.conn.SendRaw("CAP END")
		c.eventBus.Emit(events.Event{
			Type:      EventSASLFailed,
			Data:      map[string]interface{}{"network": c.network.Address, "error": "SASL authentication failed"},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// ERR_NICKNAMEINUSE (433) - nick collision handling
	c.conn.AddCallback("433", func(e ircmsg.Message) {
		c.mu.Lock()
		// On first collision, record the desired nick
		if c.desiredNick == "" {
			c.desiredNick = c.conn.Nick
		}
		attempt := c.nickAttempt
		c.nickAttempt++
		c.mu.Unlock()

		const maxAttempts = 5

		if attempt >= maxAttempts {
			c.storage.WriteMessage(storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     fmt.Sprintf("Nick %q is in use and all %d alternatives failed. Please change your nick manually.", c.desiredNick, maxAttempts),
				MessageType: "status",
				Timestamp:   time.Now(),
			})
			return
		}

		var newNick string
		if attempt == 0 {
			// First attempt: append underscore
			newNick = c.desiredNick + "_"
		} else {
			// Subsequent attempts: append a number
			newNick = fmt.Sprintf("%s%d", c.desiredNick, attempt)
		}

		logger.Log.Info().
			Str("desired", c.desiredNick).
			Str("trying", newNick).
			Int("attempt", attempt+1).
			Msg("Nickname in use, trying alternative")

		c.storage.WriteMessage(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Nick %q is already in use, trying %q...", c.conn.Nick, newNick),
			MessageType: "status",
			Timestamp:   time.Now(),
		})

		c.conn.SendRaw(fmt.Sprintf("NICK %s", newNick))
	})
}

// contains checks if a capability string contains the given capability
// getMessageTime returns the server-time from the message's IRCv3 tags if available,
// otherwise returns time.Now(). Requires the server-time capability to be enabled.
func (c *IRCClient) getMessageTime(e ircmsg.Message) time.Time {
	c.mu.RLock()
	hasServerTime := c.enabledCaps["server-time"]
	c.mu.RUnlock()

	if hasServerTime {
		if present, timeTag := e.GetTag("time"); present && timeTag != "" {
			if t, err := time.Parse(time.RFC3339Nano, timeTag); err == nil {
				return t
			}
			// Also try without fractional seconds
			if t, err := time.Parse("2006-01-02T15:04:05Z", timeTag); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

// isEchoMessage returns true if the message is an echo of our own message
// (sent back by the server because the echo-message capability is enabled).
func (c *IRCClient) isEchoMessage(e ircmsg.Message) bool {
	c.mu.RLock()
	hasEcho := c.enabledCaps["echo-message"]
	c.mu.RUnlock()

	if !hasEcho {
		return false
	}
	return strings.EqualFold(e.Nick(), c.network.Nickname)
}

// pmPeer returns the conversation peer (the other party) for a private message.
// For messages we sent (sender == our nick, e.g. echoed back via echo-message)
// the peer is the recipient (target); for received messages it is the sender.
// Uses the same identity comparison as isEchoMessage (c.network.Nickname).
func (c *IRCClient) pmPeer(sender, target string) string {
	if strings.EqualFold(sender, c.network.Nickname) {
		return target
	}
	return sender
}

// noticePMTarget decides whether an inbound NOTICE should be routed to a
// per-sender query pane and, if so, returns the conversation peer. Services such
// as ChanServ/NickServ (and ordinary users) reply via NOTICE, so those belong in
// the sender's query pane rather than the Status log.
//
// A real user/service prefix carries a "nick!user@host" hostmask; a server
// notice uses a bare server-name prefix (no '!') and pre-registration notices
// are addressed to "*". Both of those, plus channel-targeted notices, stay out
// of any PM pane (returns ""). The peer is computed with the same echo-aware
// rule as pmPeer so an echoed self-notice keys to the recipient.
func (c *IRCClient) noticePMTarget(source, sender, target string) string {
	if !strings.Contains(source, "!") {
		return "" // bare server prefix (or no prefix) -> Status
	}
	if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
		return "" // channel notice -> not a PM
	}
	return c.pmPeer(sender, target)
}

func contains(capabilities, cap string) bool {
	// Split by space and check each capability
	// Capabilities can be in the form "cap" or "cap=value" or "cap=value1,value2"
	caps := strings.Fields(capabilities)
	for _, c := range caps {
		// Check if the capability name matches (before the = sign)
		capName := c
		if idx := strings.Index(c, "="); idx != -1 {
			capName = c[:idx]
		}
		if capName == cap {
			return true
		}
	}
	return false
}

// startSASLAuth initiates SASL authentication
func (c *IRCClient) startSASLAuth() {
	c.mu.Lock()
	if c.saslInProgress {
		c.mu.Unlock()
		return
	}
	c.saslInProgress = true
	c.mu.Unlock()

	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     fmt.Sprintf("Starting SASL authentication with mechanism: %s", c.saslMechanism),
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	c.storage.WriteMessage(statusMsg)

	c.eventBus.Emit(events.Event{
		Type:      EventSASLStarted,
		Data:      map[string]interface{}{"network": c.network.Address, "mechanism": c.saslMechanism},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	// Send AUTHENTICATE with mechanism
	c.conn.SendRaw(fmt.Sprintf("AUTHENTICATE %s", c.saslMechanism))
}

// handleAUTHENTICATE handles AUTHENTICATE command responses
func (c *IRCClient) handleAUTHENTICATE(e ircmsg.Message) {
	if len(e.Params) == 0 {
		return
	}

	response := e.Params[0]

	switch c.saslMechanism {
	case "PLAIN":
		c.handlePLAINAuth(response)
	case "EXTERNAL":
		c.handleEXTERNALAuth(response)
	case "SCRAM-SHA-256", "SCRAM-SHA-512":
		c.handleSCRAMAuth(response)
	default:
		// Unknown mechanism
		c.conn.SendRaw("AUTHENTICATE *")
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
	}
}

// handlePLAINAuth handles PLAIN mechanism authentication
func (c *IRCClient) handlePLAINAuth(response string) {
	if response == "+" {
		// Server is ready for credentials
		// PLAIN format: \0username\0password (base64 encoded)
		authString := fmt.Sprintf("\x00%s\x00%s", c.saslUsername, c.saslPassword)
		encoded := base64.StdEncoding.EncodeToString([]byte(authString))
		c.conn.SendRaw(fmt.Sprintf("AUTHENTICATE %s", encoded))
	} else if response == "*" {
		// Abort
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
		c.conn.SendRaw("CAP END")
	}
}

// handleEXTERNALAuth handles EXTERNAL mechanism authentication
func (c *IRCClient) handleEXTERNALAuth(response string) {
	if response == "+" {
		// EXTERNAL uses TLS client certificate, send empty response
		c.conn.SendRaw("AUTHENTICATE +")
	} else if response == "*" {
		// Abort
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
		c.conn.SendRaw("CAP END")
	}
}

// handleSCRAMAuth handles SCRAM-SHA-256 and SCRAM-SHA-512 authentication
// Implementation is in scram.go

// Connect connects to the IRC server
func (c *IRCClient) Connect() error {
	// CAP LS 302 is sent after Connect() succeeds but before Loop()

	if err := c.conn.Connect(); err != nil {
		c.eventBus.Emit(events.Event{
			Type:      EventError,
			Data:      map[string]interface{}{"error": err.Error()},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Always send CAP LS 302 for IRCv3 capability negotiation
	c.conn.SendRaw("CAP LS 302")
	c.storage.WriteMessage(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     "Sent CAP LS 302 for IRCv3 capability negotiation",
		MessageType: "status",
		Timestamp:   time.Now(),
	})

	// Start the connection loop in a goroutine
	go c.conn.Loop()

	// Auto-join channels after a short delay to ensure connection is established
	// Only join channels that have auto_join enabled
	// Channels with is_open=true but auto_join=false remain OPEN (dialog open but not joined)
	go func() {
		time.Sleep(constants.AutoJoinDelay)
		channels, err := c.storage.GetChannels(c.networkID)
		if err == nil {
			logger.Log.Info().Int("count", len(channels)).Msg("Checking channels for auto-join")
			for _, channel := range channels {
				logger.Log.Debug().
					Str("channel", channel.Name).
					Bool("auto_join", channel.AutoJoin).
					Bool("is_open", channel.IsOpen).
					Msg("Channel auto-join status")
				// Only auto-join if auto_join is enabled
				// Channels with is_open=true but auto_join=false remain OPEN (dialog open but not joined)
				if channel.AutoJoin {
					logger.Log.Info().Str("channel", channel.Name).Msg("Auto-joining channel (auto_join enabled)")
					c.conn.Join(channel.Name)
				} else if channel.IsOpen {
					logger.Log.Debug().Str("channel", channel.Name).Msg("Channel is open but auto_join disabled - keeping OPEN state (not joining)")
				} else {
					logger.Log.Debug().Str("channel", channel.Name).Msg("Skipping channel (not open and auto-join disabled)")
				}
			}
		} else {
			logger.Log.Error().Err(err).Msg("Failed to get channels for auto-join")
		}
	}()

	return nil
}

// Disconnect disconnects from the IRC server
func (c *IRCClient) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	// Send QUIT command with reason
	if err := c.conn.SendRaw("QUIT :Disconnecting"); err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to send QUIT command, trying conn.Quit()")
		// Fallback to conn.Quit() if SendRaw fails
		c.conn.Quit()
	}
	c.connected = false
	return nil
}

// SendMessage sends a message to a channel or user
func (c *IRCClient) SendMessage(target, message string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	// Wait for rate limiter before sending
	c.rateLimiter.Wait()

	if err := c.conn.Privmsg(target, message); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Determine if it's a channel or private message
	var channelID *int64
	if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
		// Channel message - look up channel ID
		ch, err := c.storage.GetChannelByName(c.networkID, target)
		if err == nil {
			channelID = &ch.ID
		} else {
			// Channel not found in database - might be a channel we haven't joined yet
			// Store with nil channel_id for now
			logger.Log.Debug().Err(err).Str("target", target).Msg("Channel not found in database")
		}
	} else {
		// Private message - create or get PM conversation
		_, err := c.storage.GetOrCreatePMConversation(c.networkID, target, c.network.Nickname)
		if err != nil {
			logger.Log.Error().Err(err).Str("target", target).Msg("Failed to create/get PM conversation")
		}
	}

	// When echo-message is enabled, the server will echo our message back and
	// we store it in the PRIVMSG handler (canonical copy with server timestamp).
	// Otherwise, store it immediately ourselves.
	c.mu.RLock()
	hasEcho := c.enabledCaps["echo-message"]
	c.mu.RUnlock()

	if !hasEcho {
		// For a private message the peer is the recipient (we are the sender);
		// for channel messages there is no PM peer.
		var pmTarget string
		if !(len(target) > 0 && (target[0] == '#' || target[0] == '&')) {
			pmTarget = target
		}
		msg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   channelID,
			User:        c.network.Nickname,
			Message:     message,
			MessageType: "privmsg",
			Timestamp:   time.Now(),
			RawLine:     fmt.Sprintf("PRIVMSG %s :%s", target, message),
			PMTarget:    pmTarget,
		}
		if err := c.storage.WriteMessageSync(msg); err != nil {
			return fmt.Errorf("failed to store message: %w", err)
		}
	}

	// Emit event
	c.eventBus.Emit(events.Event{
		Type: EventMessageSent,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"target":    target,
			"message":   message,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	return nil
}

// JoinChannel joins an IRC channel
func (c *IRCClient) JoinChannel(channel string) error {
	// Validate channel name
	if err := validation.ValidateChannelName(channel); err != nil {
		return fmt.Errorf("invalid channel name: %w", err)
	}

	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		logger.Log.Warn().Str("channel", channel).Msg("Not connected, cannot join")
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	logger.Log.Info().
		Str("channel", channel).
		Str("network", c.network.Name).
		Str("nick", c.network.Nickname).
		Msg("Sending JOIN command for channel")
	if err := c.conn.Join(channel); err != nil {
		logger.Log.Error().Err(err).Msg("Error from ircevent.Join")
		return err
	}
	logger.Log.Info().Str("channel", channel).Msg("JOIN command sent successfully")
	return nil
}

// PartChannel leaves an IRC channel
func (c *IRCClient) PartChannel(channel string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	return c.conn.Part(channel)
}

// SetNetworkID sets the network ID for message storage
func (c *IRCClient) SetNetworkID(id int64) {
	c.networkID = id
}

// WriteStatusMessage writes a message to the status window
func (c *IRCClient) WriteStatusMessage(message string) error {
	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil, // Status window
		User:        "*",
		Message:     message,
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	return c.storage.WriteMessage(statusMsg)
}

// SendRawCommand sends a raw IRC command
func (c *IRCClient) SendRawCommand(command string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	// Wait for rate limiter before sending
	c.rateLimiter.Wait()

	// Send raw command via the connection's SendRaw method
	if err := c.conn.SendRaw(command); err != nil {
		return fmt.Errorf("failed to send raw command: %w", err)
	}

	// Store command in status window
	msg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil, // Status window
		User:        c.network.Nickname,
		Message:     command,
		MessageType: "command",
		Timestamp:   time.Now(),
		RawLine:     command,
	}

	if err := c.storage.WriteMessage(msg); err != nil {
		return fmt.Errorf("failed to store command: %w", err)
	}

	return nil
}

// parsePREFIX parses the PREFIX parameter from ISUPPORT
// Format: (ov)@+ where (ov) are the mode letters and @+ are the prefix characters
// This maps '@' -> 'o' (op) and '+' -> 'v' (voice)
func (c *IRCClient) parsePREFIX(prefixValue string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.serverCapabilities.PrefixString = prefixValue
	c.serverCapabilities.Prefix = make(map[rune]rune)

	// Find the opening parenthesis
	openParen := strings.IndexRune(prefixValue, '(')
	if openParen == -1 {
		logger.Log.Warn().
			Str("prefix", prefixValue).
			Msg("Invalid PREFIX format: missing opening parenthesis")
		return
	}

	// Find the closing parenthesis
	closeParen := strings.IndexRune(prefixValue[openParen:], ')')
	if closeParen == -1 {
		logger.Log.Warn().
			Str("prefix", prefixValue).
			Msg("Invalid PREFIX format: missing closing parenthesis")
		return
	}
	closeParen += openParen

	// Extract mode letters (between parentheses)
	modeLetters := prefixValue[openParen+1 : closeParen]

	// Extract prefix characters (after closing parenthesis)
	prefixChars := prefixValue[closeParen+1:]

	// Map prefix characters to mode letters
	// The order should match: first prefix char maps to first mode letter, etc.
	modeRunes := []rune(modeLetters)
	prefixRunes := []rune(prefixChars)

	for i := 0; i < len(modeRunes) && i < len(prefixRunes); i++ {
		c.serverCapabilities.Prefix[prefixRunes[i]] = modeRunes[i]
	}

	logger.Log.Debug().
		Str("prefix", prefixValue).
		Str("modes", modeLetters).
		Str("prefixes", prefixChars).
		Str("network", c.network.Name).
		Msg("Parsed PREFIX from ISUPPORT")
}

// GetServerCapabilities returns a copy of the server capabilities
func (c *IRCClient) GetServerCapabilities() *ServerCapabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to avoid race conditions
	cap := &ServerCapabilities{
		Prefix:       make(map[rune]rune),
		PrefixString: c.serverCapabilities.PrefixString,
		ChanModes:    c.serverCapabilities.ChanModes,
	}

	// Copy the prefix map
	for k, v := range c.serverCapabilities.Prefix {
		cap.Prefix[k] = v
	}

	return cap
}

// GetChanModeClasses returns the resolved CHANMODES classes as sorted letter strings
// (A=list, B=param, C=set-param, D=flag), applying RFC1459 defaults when the server
// never advertised CHANMODES. Used to drive the capability-aware mode editor.
func (c *IRCClient) GetChanModeClasses() (a, b, cc, d string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cls := c.serverCapabilities.classification()
	return sortedRunes(cls.List), sortedRunes(cls.Param), sortedRunes(cls.SetParam), sortedRunes(cls.Flag)
}

// RequestChannelBans asks the server for the channel ban list (MODE #channel +b).
// Results arrive asynchronously via the EventChannelBanList event after RPL_ENDOFBANLIST.
func (c *IRCClient) RequestChannelBans(channel string) error {
	return c.SendRawCommand(fmt.Sprintf("MODE %s +b", channel))
}

// handleCTCPRequest handles incoming CTCP requests and sends appropriate responses
func (c *IRCClient) handleCTCPRequest(from, command, args string) {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return
	}

	var response string
	switch command {
	case "VERSION":
		response = "Cascade IRC Client v1.0.0"
	case "TIME":
		response = time.Now().Format(time.RFC1123Z)
	case "PING":
		// Echo back the ping argument or use current timestamp
		if args != "" {
			response = args
		} else {
			response = fmt.Sprintf("%d", time.Now().Unix())
		}
	case "CLIENTINFO":
		response = "ACTION CLIENTINFO PING TIME VERSION"
	default:
		// Unknown CTCP command - don't respond
		return
	}

	// Send CTCP response as NOTICE with \001 delimiters
	ctcpResponse := fmt.Sprintf("\001%s %s\001", command, response)
	c.conn.Notice(from, ctcpResponse)

	// Log the CTCP request
	logger.Log.Debug().
		Str("from", from).
		Str("command", command).
		Str("response", response).
		Msg("Handled CTCP request")
}

// SendCTCPRequest sends a CTCP request to a target
func (c *IRCClient) SendCTCPRequest(target, command, args string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	var ctcpMessage string
	if args != "" {
		ctcpMessage = fmt.Sprintf("\001%s %s\001", command, args)
	} else {
		ctcpMessage = fmt.Sprintf("\001%s\001", command)
	}

	if err := c.conn.Privmsg(target, ctcpMessage); err != nil {
		return fmt.Errorf("failed to send CTCP request: %w", err)
	}

	// Store CTCP request in status window
	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        c.network.Nickname,
		Message:     fmt.Sprintf("CTCP %s sent to %s", command, target),
		MessageType: "ctcp",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	c.storage.WriteMessage(statusMsg)

	return nil
}
