package irc

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newAnnounceBotTestClient builds a minimal IRCClient with storage, an event
// bus, a network, and serverCapabilities — the minimum needed to call
// announceBotMode. c.conn is deliberately left nil; tests that need to verify
// a raw send set it up themselves via newConnectedPipe.
func newAnnounceBotTestClient(t *testing.T) *IRCClient {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "test.db"), 100, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	net := &storage.Network{Name: "Test", Address: "irc.test.local", Nickname: "preferrednick", CreatedAt: time.Now()}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	bus := events.NewEventBus()
	c := &IRCClient{
		eventBus:           bus,
		storage:            s,
		networkID:          net.ID,
		network:            net,
		serverCapabilities: &ServerCapabilities{},
	}
	return c
}

// newConnectedPipe creates an ircevent.Connection that is wired to a net.Pipe
// pair instead of a real server. It returns the connection (already connected
// so running=true) and a channel that receives every raw outgoing line (without
// the trailing \r\n) that the client sends. The connection is cleaned up on
// t.Cleanup.
//
// A minimal stub "server" goroutine reads the client's NICK/USER and replies
// with RPL_WELCOME (001) so that ircevent.Connection.Connect() unblocks.
func newConnectedPipe(t *testing.T) (*ircevent.Connection, <-chan string) {
	t.Helper()

	serverConn, clientConn := net.Pipe()

	// sentLines receives every raw line that the client writes to the socket.
	sentLines := make(chan string, 64)

	// Stub server: reads each line, forwards it to sentLines, and replies with
	// 001 + 376 (RPL_ENDOFMOTD) once it has seen NICK + USER. ircevent waits
	// for 376 (or 422 ERR_NOMOTD) in handleRegistration to unblock Connect().
	go func() {
		defer serverConn.Close()
		r := bufio.NewReader(serverConn)
		sentNick := false
		sentUser := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")
			// Deliver to the test (non-blocking; buffer is large enough).
			select {
			case sentLines <- trimmed:
			default:
			}
			if strings.HasPrefix(trimmed, "NICK ") {
				sentNick = true
			}
			if strings.HasPrefix(trimmed, "USER ") {
				sentUser = true
			}
			// Once both NICK + USER arrive, complete registration.
			// ircevent.Connection.Connect() blocks until it receives 376 or 422.
			if sentNick && sentUser {
				_, _ = serverConn.Write([]byte(":irc.test.local 001 robodan :Welcome\r\n"))
				_, _ = serverConn.Write([]byte(":irc.test.local 376 robodan :End of MOTD\r\n"))
				sentNick = false // reset so we don't re-send on reconnect
				sentUser = false
			}
		}
	}()

	conn := &ircevent.Connection{
		Nick:       "robodan",
		Server:     "irc.test.local:6667",
		KeepAlive:  5 * time.Minute,
		Timeout:    10 * time.Second,
		MaxLineLen: 512,
		AllowPanic: false,
		// DialContext returns clientConn so Connect() uses our pipe.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
	}

	// Connect blocks until we receive 001 from the stub server above.
	if err := conn.Connect(); err != nil {
		t.Fatalf("ircevent.Connection.Connect(): %v", err)
	}
	t.Cleanup(func() {
		conn.Quit()
		clientConn.Close()
	})

	return conn, sentLines
}

// drainUntil reads lines from sentLines until it finds one matching the prefix,
// or until the deadline. Returns the matching line or fails the test.
func drainUntilPrefix(t *testing.T, sentLines <-chan string, prefix string, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case line := <-sentLines:
			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a line starting with %q", prefix)
			return ""
		}
	}
}

// hasStatusLineInStorage returns true when the network's status buffer contains
// a message with the given type and text. It mirrors the hasStatusLine helper
// in standard_replies_test.go.
func hasStatusLineInStorage(t *testing.T, c *IRCClient, mtype, text string) bool {
	t.Helper()
	msgs, err := c.storage.GetMessages(c.networkID, nil, 20)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	for _, m := range msgs {
		if m.MessageType == mtype && m.Message == text {
			return true
		}
	}
	return false
}

// --- Tests ---

// TestAnnounceBotMode_SendsModeWhenSupportedAndEnabled verifies that when
// IdentifyAsBot is true and the server advertised a BOT= letter, announceBotMode
// sends "MODE <nick> +<letter>" as a raw command.
func TestAnnounceBotMode_SendsModeWhenSupportedAndEnabled(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	c.currentNick = "robodan"
	c.network.IdentifyAsBot = true
	c.serverCapabilities.BotModeChar = 'B'

	conn, sentLines := newConnectedPipe(t)
	c.conn = conn

	c.announceBotMode()

	// Drain lines sent to the server; NICK/USER/CAP appear first from
	// Connect(). We wait for the MODE line specifically.
	got := drainUntilPrefix(t, sentLines, "MODE ", 2*time.Second)
	if got != "MODE robodan +B" {
		t.Fatalf("unexpected MODE line: %q, want \"MODE robodan +B\"", got)
	}
}

// TestAnnounceBotMode_NoopWhenDisabled verifies that when IdentifyAsBot is
// false, announceBotMode does not send any MODE command (conn remains nil and
// no panic occurs).
func TestAnnounceBotMode_NoopWhenDisabled(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	c.currentNick = "robodan"
	c.network.IdentifyAsBot = false
	c.serverCapabilities.BotModeChar = 'B'
	// conn is nil; if announceBotMode touches it we'll panic → test failure.
	c.announceBotMode() // must not panic or send anything
}

// TestAnnounceBotMode_StatusLineWhenUnsupported verifies that when
// IdentifyAsBot is true but the server has not advertised a BOT= letter,
// announceBotMode writes a warning status line and does not send a MODE command
// (conn remains nil and no panic occurs).
func TestAnnounceBotMode_StatusLineWhenUnsupported(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	c.currentNick = "robodan"
	c.network.IdentifyAsBot = true
	// BotModeChar deliberately 0 — server did not advertise BOT=.
	c.announceBotMode() // must not panic or touch conn

	if !hasStatusLineInStorage(t, c, "warning", "Server does not support bot mode; +B not set") {
		t.Fatal("expected warning status line but it was not written to storage")
	}
}
