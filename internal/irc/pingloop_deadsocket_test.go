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
	"github.com/ergochat/irc-go/ircmsg"
)

// newShortTimeoutDeafPipe wires an ircevent.Connection to a stub server that
// completes registration (001 + 376), then drains the client's writes so they
// never block, NEVER answers a PING with a PONG, and NEVER closes the socket.
// This is the live "half-open corpse" reproduced on an AWAKE machine: a network
// blip killed the path (no FIN/EOF is ever delivered), the read parks forever,
// and the ONLY thing that can notice is the library's own PING/PONG keepalive.
//
// KeepAlive/Timeout are set tiny (unlike the 5-minute production-mirror in
// teardown_test.go) so the pingLoop's escalation — if it happens at all —
// happens within the test window instead of ~90s later.
func newShortTimeoutDeafPipe(t *testing.T) *ircevent.Connection {
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
			// Deliberately never respond to PING and never close: the corpse.
		}
	}()

	conn := &ircevent.Connection{
		Nick:       "robodan",
		Server:     "irc.test.local:6667",
		KeepAlive:  200 * time.Millisecond,
		Timeout:    100 * time.Millisecond,
		MaxLineLen: 512,
		Log:        log.New(io.Discard, "", 0),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
	}
	if err := conn.Connect(); err != nil {
		t.Fatalf("ircevent.Connection.Connect(): %v", err)
	}
	return conn
}

// TestPingLoopDetectsDeadSocketWithoutWake is the regression test for the live
// zombie observed on 2026-07-03: two IRC sockets sat in TCP CLOSED with the fd
// still held for 2.5 hours while the UI showed "Connected", on a machine that
// never slept. The system-wake ForceReconnect path (PR #140) therefore never
// fired, and the connection was never torn down.
//
// The library's contract (see IsConnected / ForceReconnect docs) is that the
// pingLoop is the single authority on liveness: it PINGs on the KeepAlive
// interval and MUST treat an unacked PING as fatal, firing the disconnect
// teardown — entirely on its own, with no wake event. This test drives exactly
// that path and asserts the disconnect fires.
func TestPingLoopDetectsDeadSocketWithoutWake(t *testing.T) {
	conn := newShortTimeoutDeafPipe(t)

	disconnected := make(chan struct{}, 1)
	conn.AddDisconnectCallback(func(ircmsg.Message) {
		select {
		case disconnected <- struct{}{}:
		default:
		}
	})

	c := &IRCClient{conn: conn, connected: true}
	c.runLoop()

	// KeepAlive=200ms, Timeout=100ms: a PING goes out at ~200ms and is declared
	// unacked one tick later. A working pingLoop tears the connection down well
	// inside a second; 2s is generous headroom. No ForceReconnect is called —
	// this isolates the pingLoop's own detection, which is what failed live.
	select {
	case <-disconnected:
		// pingLoop noticed the unacked PING and tore the dead socket down.
	case <-time.After(2 * time.Second):
		t.Fatal(`pingLoop did not tear down a silently-dead socket on its own — the connection zombies "Connected" forever unless an OS wake happens to fire ForceReconnect (the live 2.5h zombie)`)
	}
}
