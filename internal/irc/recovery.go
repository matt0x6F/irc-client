package irc

import (
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
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

// handleSASLFailure records an authentication failure and aborts the
// connection. It deliberately does NOT call endCapNegotiation: completing CAP
// negotiation would register the user as an unauthenticated guest, which is the
// failure mode this whole change exists to prevent.
func (c *IRCClient) handleSASLFailure(reason string) {
	c.mu.Lock()
	c.saslInProgress = false
	c.saslAuthenticated = false
	c.authFailed = true
	c.mu.Unlock()

	c.storage.WriteMessage(storage.Message{
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

	c.abortAuth()
}

// abortAuth tears the connection down after an authentication failure. The QUIT
// routes through the library DisconnectCallback -> EventConnectionLost, where
// the app layer sees AuthFailed() and suppresses auto-reconnect unless the user
// opted in.
func (c *IRCClient) abortAuth() {
	if err := c.conn.SendRaw("QUIT :Authentication failed"); err != nil {
		c.conn.Quit()
	}
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()
}
