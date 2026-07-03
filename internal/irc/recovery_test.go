package irc

import (
	"strings"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

func TestAuthFailedAccessors(t *testing.T) {
	c := &IRCClient{}
	if c.AuthFailed() {
		t.Fatal("new client should not report auth failed")
	}
	c.setAuthFailed(true)
	if !c.AuthFailed() {
		t.Fatal("setAuthFailed(true) should be observable via AuthFailed()")
	}
	c.setAuthFailed(false)
	if c.AuthFailed() {
		t.Fatal("setAuthFailed(false) should clear the flag")
	}
}

// TestJoinError477ClosesChannelAndWritesStatus verifies that ERR_NEEDREGGEDNICK
// (477) — the auth-relevant join failure — closes the phantom channel buffer and
// writes a status-window message explaining why. The test uses the real storage
// and event bus; conn is intentionally nil because handleJoinError never touches
// c.conn.
func TestJoinError477ClosesChannelAndWritesStatus(t *testing.T) {
	c := newNickTestClient(t)
	c.currentNick = "matt0x6f_0"

	mustCreateChannel(t, c.storage, &storage.Channel{
		NetworkID: c.networkID, Name: "#staff", IsOpen: true, CreatedAt: time.Now(),
	})

	c.handleJoinError(parseLine(t, ":server 477 matt0x6f_0 #staff :Cannot join channel (+r) - registered nick required"))

	ch, err := c.storage.GetChannelByName(c.networkID, "#staff")
	if err != nil {
		t.Fatalf("channel should still exist in storage: %v", err)
	}
	if ch.IsOpen {
		t.Fatal("a failed join (477) must leave the channel closed, not lingering open")
	}

	msgs := statusMessages(t, c)
	var found bool
	for _, m := range msgs {
		if strings.Contains(m.Message, "#staff") && strings.Contains(m.Message, "registered") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a status message mentioning #staff and 'registered'; got: %v", msgs)
	}
}

// assertSASLAbortBehavior is a shared helper that verifies any SASL abort path:
// AuthFailed() must be true, the connection must emit QUIT, and CAP END must never
// appear. The caller invokes the abort via triggerFn and this helper drains
// sentLines with a deadline.
func assertSASLAbortBehavior(t *testing.T, c *IRCClient, sentLines <-chan string, triggerFn func()) {
	t.Helper()
	triggerFn()

	if !c.AuthFailed() {
		t.Fatal("AuthFailed() must be true after SASL abort")
	}

	var sawQUIT, sawCapEnd bool
	deadline := time.After(time.Second)
	for !sawQUIT {
		select {
		case line := <-sentLines:
			if strings.HasPrefix(line, "QUIT") {
				sawQUIT = true
			}
			if strings.HasPrefix(line, "CAP END") {
				sawCapEnd = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for QUIT after SASL abort")
		}
	}
	if sawCapEnd {
		t.Fatal("SASL abort must NOT send CAP END (that registers unauthenticated)")
	}
}

// TestJoinError474ClosesChannelAndWritesStatus verifies that ERR_BANNEDFROMCHAN
// (474) also closes the phantom channel and writes an appropriate status line.
func TestJoinError474ClosesChannelAndWritesStatus(t *testing.T) {
	c := newNickTestClient(t)
	c.currentNick = "matt0x6f_0"

	mustCreateChannel(t, c.storage, &storage.Channel{
		NetworkID: c.networkID, Name: "#secret", IsOpen: true, CreatedAt: time.Now(),
	})

	c.handleJoinError(parseLine(t, ":server 474 matt0x6f_0 #secret :Cannot join channel (+b)"))

	ch, err := c.storage.GetChannelByName(c.networkID, "#secret")
	if err != nil {
		t.Fatalf("channel should still exist in storage: %v", err)
	}
	if ch.IsOpen {
		t.Fatal("a failed join (474) must leave the channel closed, not lingering open")
	}

	msgs := statusMessages(t, c)
	var found bool
	for _, m := range msgs {
		if strings.Contains(m.Message, "#secret") && strings.Contains(m.Message, "banned") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a status message mentioning #secret and 'banned'; got: %v", msgs)
	}
}
