package irc

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newDroppablePipe wires an ircevent.Connection to a net.Pipe stub server (same
// protocol behavior as newStubServerPipe) and returns a drop function that
// closes the server side unilaterally — modeling an unexpected connection loss
// (process death on the far side, TCP reset, network partition) rather than a
// QUIT-acknowledged teardown.
func newDroppablePipe(t *testing.T) (*ircevent.Connection, func()) {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	go func() {
		r := bufio.NewReader(serverConn)
		sentNick, sentUser := false, false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(trimmed, "NICK ") {
				sentNick = true
			}
			if strings.HasPrefix(trimmed, "USER ") {
				sentUser = true
			}
			if sentNick && sentUser {
				_, _ = serverConn.Write([]byte(":irc.test.local 001 robodan :Welcome\r\n"))
				_, _ = serverConn.Write([]byte(":irc.test.local 376 robodan :End of MOTD\r\n"))
				sentNick, sentUser = false, false
			}
		}
	}()

	conn := &ircevent.Connection{
		Nick:       "robodan",
		Server:     "irc.test.local:6667",
		KeepAlive:  5 * time.Minute,
		Timeout:    10 * time.Second,
		MaxLineLen: 512,
		// ReconnectFreq left at 0 to mirror production exactly: the library
		// treats 0 as "default to two minutes", NOT "disabled" — and for a
		// connection whose delay has already elapsed, Loop() re-dials
		// immediately after a drop.
		Log: log.New(io.Discard, "", 0),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
	}
	if err := conn.Connect(); err != nil {
		t.Fatalf("ircevent.Connection.Connect(): %v", err)
	}
	return conn, func() { _ = serverConn.Close() }
}

// TestUnexpectedDropStopsLibraryLoop is the regression guard for the
// nick-squatting ghost seen in the field: on an unexpected drop, the library
// Loop() must exit — not re-dial. The app is the sole reconnect owner (every
// drop is recovered with a fresh IRCClient), so a dropped client's Loop has no
// legitimate next connection. Before the fix, onDisconnect only set the
// app-side abandoned flag; the Loop then re-dialed immediately and the
// abandoned client completed a FULL registration (CAP, SASL, NICK), squatting
// the preferred nick just long enough that the app's real reconnect registered
// as nick_0 and visibly joined every channel as a ghost.
func TestUnexpectedDropStopsLibraryLoop(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "test.db"), 100, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	network := &storage.Network{Name: "ErgoIRC", Address: "irc.ergo.chat", Nickname: "matt0x6f", CreatedAt: time.Now()}
	if err := s.CreateNetwork(network); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	conn, drop := newDroppablePipe(t)
	c := &IRCClient{
		conn:      conn,
		eventBus:  events.NewEventBus(),
		storage:   s,
		networkID: network.ID,
		network:   network,
		connected: true,
	}
	// Register the production disconnect handler — the code under test.
	conn.AddDisconnectCallback(c.onDisconnect)

	c.runLoop()

	// Unexpected drop: the far side vanishes without any QUIT handshake.
	drop()

	c.mu.RLock()
	done := c.loopDone
	c.mu.RUnlock()

	select {
	case <-done:
		// Loop returned: the dropped client is fully retired, no ghost re-dial.
	case <-time.After(3 * time.Second):
		t.Fatal("Loop() did not exit after an unexpected drop — the library will re-dial and the abandoned client will re-register, squatting the nick while the app reconnects a fresh client")
	}
}
