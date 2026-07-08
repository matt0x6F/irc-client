package irc

import (
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// recovery.go holds the connection's authentication-failure state and the
// channel-join error feedback. It builds only on protocol-detectable signals
// (SASL numerics, standard join-error numerics) — it issues no services
// commands and parses no NOTICE text.

// AuthFailed reports whether SASL was enabled but did not succeed on this
// session. The app layer reads it to decide whether an auto-reconnect should be
// suppressed (a credential failure never recovers by retrying).
func (c *IRCClient) AuthFailed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authFailed
}

func (c *IRCClient) setAuthFailed(v bool) {
	c.mu.Lock()
	c.authFailed = v
	c.mu.Unlock()
}

// observeSASLSuccess records a successful authentication seen via RPL_LOGGEDIN
// (900) / RPL_SASLSUCCESS (903). It is observation-only: the library owns CAP
// negotiation and SASL and has already completed them, so this just records
// state. Safe to call more than once (900 and 903 usually both arrive); the flag
// writes and status line are harmless when repeated.
func (c *IRCClient) observeSASLSuccess() {
	c.mu.Lock()
	c.saslAuthenticated = true
	c.authFailed = false
	c.mu.Unlock()

	c.writeStatusBuffer(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     "SASL authentication successful",
		MessageType: "status",
		Timestamp:   time.Now(),
	})
	c.eventBus.Emit(events.Event{
		Type:      EventSASLSuccess,
		Data:      map[string]interface{}{"network": c.network.Address, "networkId": c.networkID},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// observeSASLFailure records an authentication failure seen via a SASL-failure
// numeric (902/904/905/906). It is observation-only: the library already tears
// the connection down and fails Connect() on such a failure, so this does NOT
// send QUIT — it only records status, emits EventSASLFailed, and sets the
// AuthFailed flag.
func (c *IRCClient) observeSASLFailure(reason string) {
	c.mu.Lock()
	c.saslAuthenticated = false
	c.authFailed = true
	c.mu.Unlock()

	c.writeStatusBuffer(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     "Authentication failed: " + reason + ". Not connecting unauthenticated.",
		MessageType: "status",
		Timestamp:   time.Now(),
	})
	c.eventBus.Emit(events.Event{
		Type:      EventSASLFailed,
		Data:      map[string]interface{}{"network": c.network.Address, "networkId": c.networkID, "error": reason},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// joinErrorReasons maps the standard join-failure numerics to a short,
// user-facing explanation. These are protocol-standard numerics — no services
// or NOTICE-text assumptions.
var joinErrorReasons = map[string]string{
	"471": "the channel is full (+l)",
	"473": "the channel is invite-only (+i)",
	"474": "you are banned from the channel",
	"475": "the channel key is wrong (+k)",
	"477": "the channel requires a registered, identified nick (+r) — identify and rejoin",
}

// handleJoinError gives feedback for a JOIN that the server rejected, mirroring
// handleForwardedJoin (470): close the phantom channel buffer so the sidebar
// reflects real membership, and write a status line explaining why. The channel
// is e.Params[1]; the server's own reason text is the trailing param.
func (c *IRCClient) handleJoinError(e ircmsg.Message) {
	if len(e.Params) < 2 {
		return
	}
	channel := e.Params[1]
	reason := joinErrorReasons[e.Command]
	if reason == "" {
		reason = "the server rejected the join"
	}
	serverText := ""
	if len(e.Params) >= 3 {
		serverText = e.Params[len(e.Params)-1]
	}

	if ch, err := c.storage.GetChannelByName(c.networkID, channel); err == nil {
		c.storage.UpdateChannelIsOpen(ch.ID, false)
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

	msg := "Couldn't join " + channel + ": " + reason + "."
	if serverText != "" {
		msg += " (" + serverText + ")"
	}
	if err := c.writeStatusBuffer(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     msg,
		MessageType: "status",
		Timestamp:   time.Now(),
	}); err != nil {
		logger.Log.Debug().Err(err).Msg("Failed to store join-error status message")
	}
}
