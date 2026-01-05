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

// IRCClient manages IRC connections
type IRCClient struct {
	conn                *ircevent.Connection
	eventBus            *events.EventBus
	storage             *storage.Storage
	networkID           int64
	network             *storage.Network
	mu                  sync.RWMutex
	connected           bool
	saslEnabled         bool
	saslMechanism       string
	saslUsername        string
	saslPassword        string
	saslInProgress      bool
	saslAuthenticated   bool
	saslCapRequested    bool
	saslCapAcknowledged bool
	scramState          *SCRAMState
	namesInProgress     map[string]bool // Track channels currently receiving NAMES list
	namesMu             sync.Mutex      // Mutex for namesInProgress map
	serverCapabilities  *ServerCapabilities // Server capabilities from ISUPPORT
	whoisInProgress     map[string]*WhoisInfo // Track WHOIS requests in progress (key: nickname)
	whoisMu             sync.Mutex      // Mutex for whoisInProgress map
}

// ServerCapabilities stores parsed ISUPPORT information
type ServerCapabilities struct {
	Prefix      map[rune]rune // Map prefix char to mode char (e.g., '@' -> 'o', '+' -> 'v')
	PrefixString string       // Raw PREFIX string (e.g., "(ov)@+")
	ChanModes   string       // Raw CHANMODES string
	mu          sync.RWMutex // Mutex for thread-safe access
}

// IsConnected returns whether the client is connected
func (c *IRCClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
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
		serverCapabilities:   &ServerCapabilities{
			Prefix:      make(map[rune]rune),
			PrefixString: "",
			ChanModes:   "",
		},
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
	}

	// Set up event handlers
	client.setupHandlers()

	return client
}

// setupHandlers sets up IRC event handlers
func (c *IRCClient) setupHandlers() {
	// Add a raw message handler to see ALL incoming messages (for debugging)
	c.conn.AddCallback("", func(e ircmsg.Message) {
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

		// If SASL is enabled, start capability negotiation
		// Note: This might be too late - CAP should be sent before connection
		// But we'll try here first and add logging
		if c.saslEnabled {
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     fmt.Sprintf("SASL enabled, sending CAP LS (mechanism: %s)", c.saslMechanism),
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     "",
			}
			c.storage.WriteMessage(statusMsg)
			// Request capability list
			c.conn.SendRaw("CAP LS 302")
		}

		// On connection established, restore channel state after restart
		// Clear all channel_users entries since we're not actually joined anymore
		if err := c.storage.ClearNetworkChannelUsers(c.networkID); err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to clear channel users on connection")
		} else {
			logger.Log.Debug().Msg("Cleared channel users on connection (restoring state after restart)")
		}

		c.eventBus.Emit(events.Event{
			Type:      EventConnectionEstablished,
			Data:      map[string]interface{}{"network": c.network.Address},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Connection lost
	c.conn.AddDisconnectCallback(func(e ircmsg.Message) {
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()

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
			Data:      map[string]interface{}{"network": c.network.Address},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
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
					if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
						ch, err := c.storage.GetChannelByName(c.networkID, channel)
						if err == nil {
							channelID = &ch.ID
						}
					}

					rawLine, _ := e.Line()
					msg := storage.Message{
						NetworkID:   c.networkID,
						ChannelID:   channelID,
						User:        user,
						Message:     fmt.Sprintf("* %s %s", user, ctcpArgs),
						MessageType: "action",
						Timestamp:   time.Now(),
						RawLine:     rawLine,
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
		// Check if this is an echo of our own message (some servers echo back sent messages)
		// If the sender is us and the target is our nickname, it's an echo - skip it
		if strings.EqualFold(user, c.network.Nickname) && strings.EqualFold(channel, c.network.Nickname) {
			// This is an echo of our own sent message - don't store it again
			// The message was already stored when we sent it
			return
		}

		// Determine if it's a channel or private message
		var channelID *int64
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
		}

		rawLine, _ := e.Line()

		msg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   channelID,
			User:        user,
			Message:     message,
			MessageType: "privmsg",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
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
				"network": c.network.Address,
				"channel": channel,
				"user":    user,
				"message": message,
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
			logger.Log.Debug().Msg("Our user joining, requesting NAMES list to update user list")
			// Request NAMES to get full user list - this will update the channel_users table
			// We don't clear first because we want GetJoinedChannels to work immediately
			c.conn.SendRaw(fmt.Sprintf("NAMES %s", channel))
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
					"network": c.network.Address,
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
					"network": c.network.Address,
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

		// Get channel from database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err != nil {
			return
		}

		// On first NAMES response for a channel, clear the existing user list
		// This ensures we start fresh when receiving the NAMES list
		c.namesMu.Lock()
		if !c.namesInProgress[channel] {
			c.namesInProgress[channel] = true
			c.namesMu.Unlock()
			if err := c.storage.ClearChannelUsers(ch.ID); err != nil {
				logger.Log.Error().Err(err).Str("channel", channel).Msg("Failed to clear users for channel")
			} else {
				logger.Log.Debug().Str("channel", channel).Msg("Cleared user list for channel before receiving NAMES")
			}
		} else {
			c.namesMu.Unlock()
		}

		// Parse names (format: "@nick1 +nick2 nick3")
		names := strings.Fields(namesList)
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
				c.storage.AddChannelUser(ch.ID, nickname, modes)
			}
		}
	})

	// RPL_ENDOFNAMES (366) - end of NAMES list
	c.conn.AddCallback("366", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[1]
		logger.Log.Debug().Str("channel", channel).Msg("Finished receiving user list for channel")

		// Mark that we're done receiving NAMES for this channel
		c.namesMu.Lock()
		delete(c.namesInProgress, channel)
		c.namesMu.Unlock()

		// Emit event when NAMES list is complete so frontend can refresh user list
		c.eventBus.Emit(events.Event{
			Type: "channel.names.complete",
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
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
		mode := e.Params[1]

		// Only handle channel modes (target starts with # or &)
		if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
			ch, err := c.storage.GetChannelByName(c.networkID, target)
			if err == nil {
				// Update channel modes
				// Note: This is simplified - full MODE parsing is complex
				c.storage.UpdateChannelModes(ch.ID, mode)

				// Emit event for mode change
				c.eventBus.Emit(events.Event{
					Type: EventChannelMode,
					Data: map[string]interface{}{
						"network": c.network.Address,
						"channel": target,
						"modes":   mode,
					},
					Timestamp: time.Now(),
					Source:    events.EventSourceIRC,
				})
			}
		}
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
					Timestamp:   time.Now(),
					RawLine:     rawLine,
				}
				c.storage.WriteMessage(statusMsg)
				return
			}
		}

		// Regular NOTICE
		// If target is our nickname or starts with #, it's a channel notice
		// Otherwise, it's a server notice - store in status window
		if target == c.network.Nickname || (len(target) > 0 && target[0] != '#' && target[0] != '&') {
			rawLine, _ := e.Line()
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil, // Status window
				User:        user,
				Message:     notice,
				MessageType: "notice",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			c.storage.WriteMessage(statusMsg)
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
				c.mu.Lock()
				c.serverCapabilities.ChanModes = chanModesValue
				c.mu.Unlock()
				logger.Log.Debug().
					Str("chanmodes", chanModesValue).
					Str("network", c.network.Name).
					Msg("Parsed CHANMODES from ISUPPORT")
			}
		}
	})

	// SASL capability negotiation
	if c.saslEnabled {
		// Track CAP LS responses (may be multi-line)
		var capLSBuffer strings.Builder

		// Handle CAP LS (list capabilities)
		c.conn.AddCallback("CAP", func(e ircmsg.Message) {
			// Log all CAP messages for debugging
			rawLine, _ := e.Line()
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     fmt.Sprintf("CAP received: %s", rawLine),
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			c.storage.WriteMessage(statusMsg)

			if len(e.Params) < 2 {
				return
			}
			subcommand := e.Params[1]

			switch subcommand {
			case "LS":
				// Server is listing capabilities (may be multi-line)
				// Format: :server CAP * LS :cap1 cap2 cap3
				// Or continuation: :server CAP * LS * :cap4 cap5
				if len(e.Params) >= 3 {
					capabilities := e.Params[2]

					// Check if this is a continuation line (has "*" as third param)
					// Format: CAP * LS * :more caps (continuation)
					// Format: CAP * LS :caps (last line)
					isContinuation := len(e.Params) > 3 && e.Params[2] == "*"

					if isContinuation {
						// This is a continuation, append to buffer
						capLSBuffer.WriteString(capabilities)
						capLSBuffer.WriteString(" ")
					} else {
						// This is the last line (or single line response)
						capLSBuffer.WriteString(capabilities)
						allCaps := capLSBuffer.String()
						capLSBuffer.Reset()

						statusMsg := storage.Message{
							NetworkID:   c.networkID,
							ChannelID:   nil,
							User:        "*",
							Message:     fmt.Sprintf("CAP LS response: %s", allCaps),
							MessageType: "status",
							Timestamp:   time.Now(),
							RawLine:     "",
						}
						c.storage.WriteMessage(statusMsg)

						// Check if server supports SASL
						if contains(allCaps, "sasl") && !c.saslCapRequested {
							statusMsg = storage.Message{
								NetworkID:   c.networkID,
								ChannelID:   nil,
								User:        "*",
								Message:     "Server supports SASL, requesting capability",
								MessageType: "status",
								Timestamp:   time.Now(),
								RawLine:     "",
							}
							c.storage.WriteMessage(statusMsg)
							// Request SASL capability
							c.conn.SendRaw("CAP REQ sasl")
							c.saslCapRequested = true
						} else if !contains(allCaps, "sasl") {
							statusMsg = storage.Message{
								NetworkID:   c.networkID,
								ChannelID:   nil,
								User:        "*",
								Message:     "Server does not support SASL, ending CAP negotiation",
								MessageType: "status",
								Timestamp:   time.Now(),
								RawLine:     "",
							}
							c.storage.WriteMessage(statusMsg)
							// SASL not supported, end capability negotiation
							c.conn.SendRaw("CAP END")
						}
					}
				}
			case "ACK":
				// Server acknowledged our capability request
				if len(e.Params) >= 3 {
					capabilities := e.Params[2]
					if contains(capabilities, "sasl") {
						c.saslCapAcknowledged = true
						// Start SASL authentication
						c.startSASLAuth()
					} else {
						// SASL not in ACK, end capability negotiation
						c.conn.SendRaw("CAP END")
					}
				}
			case "NAK":
				// Server rejected our capability request
				if len(e.Params) >= 3 {
					capabilities := e.Params[2]
					if contains(capabilities, "sasl") {
						// SASL not available, continue without it
						c.conn.SendRaw("CAP END")
					}
				}
			}
		})

		// Handle AUTHENTICATE responses
		c.conn.AddCallback("AUTHENTICATE", func(e ircmsg.Message) {
			c.handleAUTHENTICATE(e)
		})

		// Handle SASL numeric replies
		// 900 RPL_LOGGEDIN - SASL authentication successful
		c.conn.AddCallback("900", func(e ircmsg.Message) {
			c.mu.Lock()
			c.saslAuthenticated = true
			c.saslInProgress = false
			c.mu.Unlock()

			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     "SASL authentication successful",
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     "",
			}
			c.storage.WriteMessage(statusMsg)

			// End capability negotiation
			c.conn.SendRaw("CAP END")

			c.eventBus.Emit(events.Event{
				Type:      EventSASLSuccess,
				Data:      map[string]interface{}{"network": c.network.Address},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		})

		// 901 RPL_LOGGEDOUT - SASL authentication failed
		c.conn.AddCallback("901", func(e ircmsg.Message) {
			c.mu.Lock()
			c.saslInProgress = false
			c.mu.Unlock()

			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     "SASL authentication failed",
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     "",
			}
			c.storage.WriteMessage(statusMsg)

			c.conn.SendRaw("CAP END")

			c.eventBus.Emit(events.Event{
				Type:      EventSASLFailed,
				Data:      map[string]interface{}{"network": c.network.Address, "error": "Authentication failed"},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		})

		// 904 ERR_SASLFAIL - SASL authentication failed
		c.conn.AddCallback("904", func(e ircmsg.Message) {
			c.mu.Lock()
			c.saslInProgress = false
			c.mu.Unlock()

			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     "SASL authentication failed",
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     "",
			}
			c.storage.WriteMessage(statusMsg)

			c.conn.SendRaw("CAP END")

			c.eventBus.Emit(events.Event{
				Type:      EventSASLFailed,
				Data:      map[string]interface{}{"network": c.network.Address, "error": "SASL authentication failed"},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		})
	}
}

// contains checks if a capability string contains the given capability
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
	// If SASL is enabled, send CAP LS immediately after TCP connection
	// This needs to happen before the server sends the welcome message
	if c.saslEnabled {
		// We'll send it in a goroutine right after Connect() to catch it early
		// But first, let's try sending it before Connect() if possible
		// Actually, we need the connection to be established first
		// So we'll send it right after Connect() succeeds but before Loop()
	}

	if err := c.conn.Connect(); err != nil {
		c.eventBus.Emit(events.Event{
			Type:      EventError,
			Data:      map[string]interface{}{"error": err.Error()},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
		return fmt.Errorf("failed to connect: %w", err)
	}

	// If SASL is enabled, send CAP LS immediately
	// This should happen before the server sends welcome messages
	if c.saslEnabled {
		c.conn.SendRaw("CAP LS 302")
		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     "Sent CAP LS 302 for SASL negotiation",
			MessageType: "status",
			Timestamp:   time.Now(),
			RawLine:     "",
		}
		c.storage.WriteMessage(statusMsg)
	}

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

	c.conn.Quit()
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
	}

	// Store sent message
	msg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   channelID,
		User:        c.network.Nickname,
		Message:     message,
		MessageType: "privmsg",
		Timestamp:   time.Now(),
		RawLine:     fmt.Sprintf("PRIVMSG %s :%s", target, message),
	}

	// Use WriteMessageSync for sent messages so they're immediately available
	if err := c.storage.WriteMessageSync(msg); err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	// Emit event
	c.eventBus.Emit(events.Event{
		Type: EventMessageSent,
		Data: map[string]interface{}{
			"network": c.network.Address,
			"target":  target,
			"message": message,
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
		Prefix:      make(map[rune]rune),
		PrefixString: c.serverCapabilities.PrefixString,
		ChanModes:   c.serverCapabilities.ChanModes,
	}

	// Copy the prefix map
	for k, v := range c.serverCapabilities.Prefix {
		cap.Prefix[k] = v
	}

	return cap
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
		response = fmt.Sprintf("Cascade IRC Client v1.0.0")
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
