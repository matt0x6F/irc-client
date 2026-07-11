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

// newPingReplyPipe is a stub server that completes registration AND answers our
// keepalive PING with a matching PONG — a live link. It models a TCP connection that
// macOS kept alive across sleep (TCPKeepAlive=active). ProbeAlive must report such a
// link as alive so the wake path leaves it untouched instead of force-reconnecting
// and rejoining every channel (the ##chat join-spam the users saw on maintenance
// DarkWakes).
func newPingReplyPipe(t *testing.T) *ircevent.Connection {
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
			if strings.HasPrefix(trimmed, "PING") {
				param := strings.TrimSpace(strings.TrimPrefix(trimmed, "PING"))
				param = strings.TrimPrefix(param, ":")
				_, _ = serverConn.Write([]byte(":irc.test.local PONG irc.test.local :" + param + "\r\n"))
			}
		}
	}()

	conn := &ircevent.Connection{
		Nick:       "robodan",
		Server:     "irc.test.local:6667",
		KeepAlive:  5 * time.Minute,
		Timeout:    10 * time.Second,
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

// TestProbeAliveTrueWhenLinkAnswers locks in the wake-churn fix: a connection that
// still answers our keepalive PING (e.g. one macOS kept alive across a maintenance
// DarkWake) must be reported alive, so reconnectAllOnWake leaves it in place rather
// than force-dropping it and rejoining channels.
func TestProbeAliveTrueWhenLinkAnswers(t *testing.T) {
	conn := newPingReplyPipe(t)
	c := &IRCClient{conn: conn, connected: true}

	if !c.ProbeAlive(2 * time.Second) {
		t.Fatal("ProbeAlive returned false for a link that answered our PING; the wake path would needlessly force-reconnect it and rejoin every channel")
	}
}

// TestProbeAliveFalseWhenLinkSilent is the other half: a sleep-killed link that never
// answers must be reported dead so the wake path still reconnects it.
func TestProbeAliveFalseWhenLinkSilent(t *testing.T) {
	conn := newStubServerPipe(t, false) // registers, then ignores PING
	c := &IRCClient{conn: conn, connected: true}

	if c.ProbeAlive(500 * time.Millisecond) {
		t.Fatal("ProbeAlive returned true for a silent/dead link; the wake path would fail to reconnect it")
	}
}
