package irc

import (
	"testing"
	"time"

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
