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

// TestSelfIdentityUsesLiveNick is the regression guard for the nick-collision
// routing bug: while connected under a fallback nick (the preferred nick was
// taken, so the server assigned "matt0x6f_0"), self-identification must compare
// against the LIVE nick (currentNick), not the preferred nick (network.Nickname).
//
// With the buggy preferred-nick comparison, an echoed PRIVMSG we sent to NickServ
// arrives as ":matt0x6f_0 PRIVMSG NickServ ...", whose sender ("matt0x6f_0") does
// not equal the preferred nick ("matt0x6f"), so it is misread as an inbound
// message from a stranger and routed into a self-DM instead of NickServ's pane.
func TestSelfIdentityUsesLiveNick(t *testing.T) {
	// Preferred nick is matt0x6f, but the server assigned the fallback matt0x6f_0.
	c := &IRCClient{network: &storage.Network{Nickname: "matt0x6f"}, currentNick: "matt0x6f_0"}

	t.Run("echoed PM keys to the recipient, not a self-DM", func(t *testing.T) {
		if got := c.pmPeer("matt0x6f_0", "NickServ"); got != "NickServ" {
			t.Fatalf("pmPeer(live-nick sender, NickServ) = %q, want %q (echo routed to self-DM)", got, "NickServ")
		}
	})

	t.Run("received PM still keys to the sender", func(t *testing.T) {
		if got := c.pmPeer("alice", "matt0x6f_0"); got != "alice" {
			t.Fatalf("pmPeer(alice, live-nick) = %q, want %q", got, "alice")
		}
	})

	t.Run("echoed self-notice keys to the recipient", func(t *testing.T) {
		if got := c.noticePMTarget("matt0x6f_0!~m@host", "matt0x6f_0", "NickServ"); got != "NickServ" {
			t.Fatalf("noticePMTarget(echoed self-notice under live nick) = %q, want %q", got, "NickServ")
		}
	})
}

func TestIsCurrentNickUsesServerCaseMapping(t *testing.T) {
	c := &IRCClient{network: &storage.Network{Nickname: "Nick[One]"}, currentNick: "Nick[One]"}
	if !c.IsCurrentNick("nick{one}") {
		t.Fatal("RFC1459-equivalent nick was not recognized as self")
	}
	mapping := "ascii"
	c.caseMappingAtomic.Store(&mapping)
	if c.IsCurrentNick("nick{one}") {
		t.Fatal("ASCII CASEMAPPING treated brackets and braces as equal")
	}
}

// TestSelfPartUnderFallbackNickClosesChannel is the regression guard for the
// phantom-channel symptom: while connected under a fallback nick, our OWN PART
// must mark the channel closed so it leaves the sidebar. With the preferred-nick
// comparison, the PART arrives as ":matt0x6f_0 PART #ircv3" — sender != preferred
// nick "matt0x6f" — so it was not recognized as us leaving, and #ircv3 lingered.
func TestSelfPartUnderFallbackNickClosesChannel(t *testing.T) {
	c := newNickTestClient(t) // preferred nick "matt0x6f"
	c.currentNick = "matt0x6f_0"

	mustCreateChannel(t, c.storage, &storage.Channel{
		NetworkID: c.networkID, Name: "#ircv3", IsOpen: true, CreatedAt: time.Now(),
	})

	// The server tells us WE parted, under our live (fallback) nick.
	c.handlePart(parseLine(t, ":matt0x6f_0!~m@host PART #ircv3"))

	ch, err := c.storage.GetChannelByName(c.networkID, "#ircv3")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if ch.IsOpen {
		t.Fatalf("#ircv3 still open after our own PART under fallback nick %q (phantom channel)", c.currentNick)
	}
}

// TestForwardedJoinClosesRequestedChannel guards the channel-forwarding case
// (Libera +f): joining "#chat" while banned/forwarded yields a 470 redirect to
// "##chat-overflow" plus a JOIN to the latter. The requested "#chat" buffer was
// never actually joined, so it must be closed — otherwise it lingers in the
// sidebar masquerading as a real channel (shown as "#chat" while WHOIS reports
// only "##chat-overflow").
func TestForwardedJoinClosesRequestedChannel(t *testing.T) {
	c := newNickTestClient(t)
	c.currentNick = "matt0x6f_0"

	// We have a stored, open #chat buffer (e.g. from auto-join).
	mustCreateChannel(t, c.storage, &storage.Channel{
		NetworkID: c.networkID, Name: "#chat", IsOpen: true, CreatedAt: time.Now(),
	})

	c.handleForwardedJoin(parseLine(t, ":lithium.libera.chat 470 matt0x6f_0 #chat ##chat-overflow :Forwarding to another channel"))

	ch, err := c.storage.GetChannelByName(c.networkID, "#chat")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if ch.IsOpen {
		t.Fatalf("requested #chat still open after 470 forward to ##chat-overflow (stale buffer)")
	}
}

// A PART by someone ELSE must NOT close our channel.
func TestOtherUserPartLeavesChannelOpen(t *testing.T) {
	c := newNickTestClient(t)
	c.currentNick = "matt0x6f_0"

	mustCreateChannel(t, c.storage, &storage.Channel{
		NetworkID: c.networkID, Name: "#ircv3", IsOpen: true, CreatedAt: time.Now(),
	})

	c.handlePart(parseLine(t, ":alice!~a@host PART #ircv3"))

	ch, err := c.storage.GetChannelByName(c.networkID, "#ircv3")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if !ch.IsOpen {
		t.Fatalf("#ircv3 wrongly closed after another user's PART")
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
// in N minutes" activity check that flipped connected -> false. That created an
// asymmetric latch — once tripped, live inbound traffic could never restore the
// reported state (#13). Liveness is now owned solely by the library's pingLoop +
// DisconnectCallback, so IsConnected() must be a pure read of the connection flag.
func TestIsConnectedIsPureGetter(t *testing.T) {
	t.Run("is side-effect free for a connected client", func(t *testing.T) {
		c := &IRCClient{connected: true}

		if !c.IsConnected() {
			t.Fatal("IsConnected() returned false for a connected client")
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
		c := &IRCClient{connected: true}
		if c.IsConnected() != c.IsConnectedDirect() {
			t.Fatal("IsConnectedDirect() must be identical to IsConnected()")
		}
	})
}

// The library's pingLoop sends a keepalive PING after KeepAlive of idle and
// declares the connection dead if it is unacked within Timeout, so it requires
// KeepAlive >= Timeout. This guards that invariant on our configured values.
func TestLivenessConstantOrdering(t *testing.T) {
	if constants.ConnectionKeepAlive < constants.ConnectionReadTimeout {
		t.Fatalf("KeepAlive (%v) must be >= Timeout (%v)", constants.ConnectionKeepAlive, constants.ConnectionReadTimeout)
	}
}

func TestBuildSendTags(t *testing.T) {
	got := buildSendTags("parent-1", "#dev")
	if got["+draft/reply"] != "parent-1" {
		t.Fatalf("reply tag missing: %v", got)
	}
	if got["+draft/channel-context"] != "#dev" {
		t.Fatalf("context tag missing: %v", got)
	}
	if len(buildSendTags("", "")) != 0 {
		t.Fatal("empty inputs must yield no tags (nil/empty map)")
	}
}
