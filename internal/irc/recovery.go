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

// handleSASLSuccess records a successful authentication, clears any prior
// auth-failure marker, and completes CAP negotiation. Triggered by both
// RPL_LOGGEDIN (900) and RPL_SASLSUCCESS (903). It is safe to call more than
// once: endCapNegotiation is idempotent, and the flag assignments are harmless
// when repeated.
func (c *IRCClient) handleSASLSuccess() {
	c.mu.Lock()
	c.saslAuthenticated = true
	c.saslInProgress = false
	c.authFailed = false
	c.mu.Unlock()

	c.storage.WriteMessage(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     "SASL authentication successful",
		MessageType: "status",
		Timestamp:   time.Now(),
	})
	c.endCapNegotiation()
	c.eventBus.Emit(events.Event{
		Type:      EventSASLSuccess,
		Data:      map[string]interface{}{"network": c.network.Address, "networkId": c.networkID},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
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

// handleLoggedOut processes RPL_LOGGEDOUT (901). The numeric is overloaded: during
// the SASL handshake it means authentication failed and we must abort rather than
// register unauthenticated; after registration it is a benign account-state change.
// Services emit a post-registration 901 as a normal event — notably Libera during a
// NickServ REGAIN, as it re-binds the session's account — and treating that as a
// failure tears down a healthy connection, forcing a reconnect that wipes every
// channel roster. So only abort while SASL is actually in progress.
func (c *IRCClient) handleLoggedOut() {
	c.mu.RLock()
	authing := c.saslEnabled && c.saslInProgress
	c.mu.RUnlock()
	if !authing {
		return
	}
	c.handleSASLFailure("logged out")
}

// handleCapLSMissingSASL aborts when SASL is required but the server's CAP LS
// did not advertise it. Without this the handshake would proceed and register
// the user unauthenticated. allCaps is the space-separated CAP LS token list.
func (c *IRCClient) handleCapLSMissingSASL(allCaps string) {
	c.mu.RLock()
	required := c.saslEnabled && !c.capNegotiationDone
	c.mu.RUnlock()
	if required && !contains(allCaps, "sasl") {
		c.handleSASLFailure("server does not support SASL")
	}
}

// abortAuth tears the connection down after an authentication failure. It routes
// through signalQuit so the library quit flag is set — otherwise the Loop
// goroutine would auto-reconnect on the resulting socket close and retry the same
// failing credentials forever (and ghost the nick). The QUIT routes through the
// library DisconnectCallback -> EventConnectionLost, where the app layer sees
// AuthFailed() and suppresses auto-reconnect unless the user opted in. It does
// not wait for the Loop to exit: abortAuth runs inside a SASL callback on the
// Loop goroutine, where waiting would deadlock.
func (c *IRCClient) abortAuth() {
	c.signalQuit("Authentication failed")
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
	if err := c.storage.WriteMessageSync(storage.Message{
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
