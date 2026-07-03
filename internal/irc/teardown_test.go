package irc

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
)

// newQuitAwarePipe wires an ircevent.Connection to a net.Pipe whose stub server
// completes registration (001 + 376) and then, like a real IRC server, CLOSES
// the connection as soon as it receives a QUIT. Closing the socket is what ends
// the client's read loop; whether the library's Loop() then exits or
// auto-reconnects is governed solely by its quit flag — which is the behavior
// under test.
func newQuitAwarePipe(t *testing.T) *ircevent.Connection {
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
			// A real server drops the link on QUIT. That ends the client read
			// loop; the library decides reconnect-vs-exit from its quit flag.
			if strings.HasPrefix(trimmed, "QUIT") {
				_ = serverConn.Close()
				return
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
		// treats 0 as "default to two minutes", NOT "disabled". So a Loop that
		// fails to see the quit flag parks for ~2 minutes before reconnecting —
		// which is what makes a leaked teardown observable as a goroutine that
		// never returns within the test's window.
		Log: log.New(io.Discard, "", 0),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
	}
	if err := conn.Connect(); err != nil {
		t.Fatalf("ircevent.Connection.Connect(): %v", err)
	}
	return conn
}

// TestDisconnectTerminatesLoopWithoutReconnect is the regression guard for the
// ghost-session bug: tearing a client down must stop the library's Loop()
// goroutine, not leave it alive to auto-reconnect under a fallback nick and hold
// the preferred nick hostage. Run Loop() exactly as production does, disconnect,
// and assert the goroutine actually exits.
func TestDisconnectTerminatesLoopWithoutReconnect(t *testing.T) {
	conn := newQuitAwarePipe(t)
	c := &IRCClient{conn: conn, connected: true}

	c.runLoop()

	if err := c.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	c.mu.RLock()
	done := c.loopDone
	c.mu.RUnlock()

	select {
	case <-done:
		// Loop returned: connection is fully torn down, no ghost left behind.
	case <-time.After(3 * time.Second):
		t.Fatal("Loop() did not exit after Disconnect() — the connection will auto-reconnect as a ghost session holding our nick")
	}
}

// loopExits runs the library Loop as production does and reports whether it
// exits within the timeout after the given teardown action — i.e. whether the
// teardown left a ghost behind.
func loopExits(t *testing.T, teardown func(c *IRCClient)) bool {
	t.Helper()
	conn := newQuitAwarePipe(t)
	c := &IRCClient{conn: conn, connected: true}
	c.runLoop()

	teardown(c)

	c.mu.RLock()
	done := c.loopDone
	c.mu.RUnlock()

	select {
	case <-done:
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

// TestStopReconnectTerminatesLoop guards the abandon-without-redial path (a drop
// we deliberately won't reconnect, e.g. AutoConnect off): the orphaned Loop must
// be stopped so it cannot silently reconnect minutes later.
func TestStopReconnectTerminatesLoop(t *testing.T) {
	if !loopExits(t, func(c *IRCClient) { c.StopReconnect() }) {
		t.Fatal("Loop() did not exit after StopReconnect() — the abandoned connection will auto-reconnect as a ghost")
	}
}
