package irc

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/constants"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// TestPMPeer verifies the conversation-peer computation used to route private
// messages. For messages we sent (sender == our nick, e.g. echoed back via
// echo-message) the peer is the recipient; for received messages it is the
// sender. The comparison is case-insensitive on our nick.
func TestPMPeer(t *testing.T) {
	c := &IRCClient{network: &storage.Network{Nickname: "matt0x6f"}}

	cases := []struct {
		name, sender, target, want string
	}{
		{"sent to chanserv (echo)", "matt0x6f", "chanserv", "chanserv"},
		{"sent, nick case differs", "MATT0X6F", "chanserv", "chanserv"},
		{"received from alice", "alice", "matt0x6f", "alice"},
		{"genuine self-PM", "matt0x6f", "matt0x6f", "matt0x6f"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.pmPeer(tc.sender, tc.target); got != tc.want {
				t.Fatalf("pmPeer(%q, %q) = %q, want %q", tc.sender, tc.target, got, tc.want)
			}
		})
	}
}

// TestNoticePMTarget verifies how an inbound NOTICE is routed. Services such as
// ChanServ/NickServ (and ordinary users) reply via NOTICE, and those should land
// in the sender's query pane — keyed by the conversation peer — rather than the
// Status log. True *server* notices (a bare server-name prefix with no
// user@host mask) and channel notices must stay out of any PM pane (peer == "").
func TestNoticePMTarget(t *testing.T) {
	c := &IRCClient{network: &storage.Network{Nickname: "matt0x6f"}}

	cases := []struct {
		name, source, sender, target, want string
	}{
		// Service reply with a full hostmask -> ChanServ's query pane.
		{"chanserv service notice", "ChanServ!ChanServ@services.libera.chat", "ChanServ", "matt0x6f", "ChanServ"},
		{"nickserv service notice", "NickServ!NickServ@services.", "NickServ", "matt0x6f", "NickServ"},
		// Bare-nick service source (no user@host, no dot) -> still a service pane.
		{"bare chanserv source", "ChanServ", "ChanServ", "matt0x6f", "ChanServ"},
		{"bare nickserv source", "NickServ", "NickServ", "matt0x6f", "NickServ"},
		// Ordinary user notice -> that user's pane.
		{"user notice", "alice!~a@host", "alice", "matt0x6f", "alice"},
		// Echoed self-notice (echo-message): peer is the recipient, not us.
		{"echoed self notice", "matt0x6f!~m@host", "matt0x6f", "chanserv", "chanserv"},
		// Server-sourced notices use a dotted host name -> stay in Status.
		{"server notice to nick", "irc.libera.chat", "irc.libera.chat", "matt0x6f", ""},
		{"pre-registration server notice", "irc.libera.chat", "irc.libera.chat", "*", ""},
		{"empty source", "", "", "*", ""},
		// A bare-nick service still must not hijack a broadcast/pre-reg notice.
		{"bare source to star target", "Global", "Global", "*", ""},
		// Channel-targeted notice is never a PM.
		{"channel notice", "alice!~a@host", "alice", "#chan", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.noticePMTarget(tc.source, tc.sender, tc.target); got != tc.want {
				t.Fatalf("noticePMTarget(%q, %q, %q) = %q, want %q",
					tc.source, tc.sender, tc.target, got, tc.want)
			}
		})
	}
}

// TestIsConnectedIsPureGetter is a regression guard for the sleep/wake bug where
// the UI reported "disconnected" while messages kept arriving.
//
// IsConnected() used to be a getter with a side effect: a passive "no messages
// in 15 minutes" check that flipped connected -> false. Combined with the
// short-circuit `if !connected { return false }`, this created an asymmetric
// latch — once tripped, live inbound traffic (which updates lastMessageTime but
// not connected) could never restore the reported state. Liveness is now owned
// solely by the library's pingLoop + DisconnectCallback, so IsConnected() must
// be a pure read of the connection flag and must ignore lastMessageTime.
func TestIsConnectedIsPureGetter(t *testing.T) {
	t.Run("stale lastMessageTime does not flip a connected client", func(t *testing.T) {
		c := &IRCClient{
			connected:       true,
			lastMessageTime: time.Now().Add(-24 * time.Hour), // far past, well beyond the old 15m threshold
		}

		if !c.IsConnected() {
			t.Fatal("IsConnected() returned false for a connected client with stale lastMessageTime; " +
				"a stale activity timestamp must no longer mark the connection dead")
		}

		// Calling it again must observe no state mutation (it is side-effect free).
		if !c.IsConnected() {
			t.Fatal("IsConnected() became false on a second call; it must not mutate connection state")
		}
		if !c.connected {
			t.Fatal("IsConnected() mutated the connected flag; it must be a pure getter")
		}
	})

	t.Run("reflects the connected flag", func(t *testing.T) {
		c := &IRCClient{connected: false}
		if c.IsConnected() {
			t.Fatal("IsConnected() returned true for a disconnected client")
		}

		// Simulate the DisconnectCallback / ConnectCallback flipping the flag.
		c.mu.Lock()
		c.connected = true
		c.mu.Unlock()
		if !c.IsConnected() {
			t.Fatal("IsConnected() did not reflect connected=true")
		}

		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()
		if c.IsConnected() {
			t.Fatal("IsConnected() did not reflect connected=false")
		}
	})

	t.Run("IsConnectedDirect agrees with IsConnected", func(t *testing.T) {
		c := &IRCClient{connected: true, lastMessageTime: time.Now().Add(-24 * time.Hour)}
		if c.IsConnected() != c.IsConnectedDirect() {
			t.Fatal("IsConnectedDirect() must be identical to IsConnected()")
		}
	})
}

// The library requires KeepAlive >= Timeout; the watchdog must only fire after
// a healthy idle link would have already PINGed, so StaleThreshold > KeepAlive.
func TestLivenessConstantOrdering(t *testing.T) {
	if constants.ConnectionKeepAlive < constants.ConnectionReadTimeout {
		t.Fatalf("KeepAlive (%v) must be >= Timeout (%v)", constants.ConnectionKeepAlive, constants.ConnectionReadTimeout)
	}
	if constants.ConnectionStaleThreshold <= constants.ConnectionKeepAlive {
		t.Fatalf("StaleThreshold (%v) must be > KeepAlive (%v)", constants.ConnectionStaleThreshold, constants.ConnectionKeepAlive)
	}
}
