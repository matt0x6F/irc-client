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

// TestSASLFailureAbortsWithoutCapEnd verifies that handleSASLFailure:
//   - sets AuthFailed()
//   - tears the connection down with QUIT
//   - never sends CAP END (which would register the user unauthenticated)
func TestSASLFailureAbortsWithoutCapEnd(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true

	c.handleSASLFailure("invalid credentials")

	if !c.AuthFailed() {
		t.Fatal("handleSASLFailure must set AuthFailed")
	}

	// Collect every sent line up to and including the QUIT; assert the abort
	// tore the connection down (QUIT) and never completed registration (no CAP END).
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
			t.Fatal("timed out waiting for QUIT after SASL failure abort")
		}
	}
	if sawCapEnd {
		t.Fatal("SASL failure must NOT send CAP END (that registers unauthenticated)")
	}
}

// TestSASLSuccessClearsAuthFailed verifies that handleSASLSuccess:
//   - clears AuthFailed (in case a prior failure was recorded on the same object)
//   - sets saslAuthenticated
//   - sends CAP END exactly once (endCapNegotiation is idempotent)
func TestSASLSuccessClearsAuthFailed(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true
	c.setAuthFailed(true) // simulate a prior failed attempt on the same client object

	c.handleSASLSuccess()

	if c.AuthFailed() {
		t.Fatal("a successful SASL result must clear AuthFailed")
	}
	c.mu.RLock()
	ok := c.saslAuthenticated
	c.mu.RUnlock()
	if !ok {
		t.Fatal("handleSASLSuccess must set saslAuthenticated")
	}

	// Drain lines for up to 1s; assert CAP END was sent exactly once.
	var capEndCount int
	deadline := time.After(time.Second)
drain:
	for {
		select {
		case line := <-sentLines:
			if strings.HasPrefix(line, "CAP END") {
				capEndCount++
			}
		case <-deadline:
			break drain
		}
	}
	if capEndCount != 1 {
		t.Fatalf("expected CAP END sent exactly once, got %d", capEndCount)
	}
}

// TestSASLSuccessIdempotent verifies that calling handleSASLSuccess twice (as
// happens when both RPL_LOGGEDIN/900 and RPL_SASLSUCCESS/903 arrive) sends
// CAP END only once.
func TestSASLSuccessIdempotent(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true

	c.handleSASLSuccess()
	c.handleSASLSuccess() // second call — must not send a second CAP END

	var capEndCount int
	deadline := time.After(time.Second)
drain:
	for {
		select {
		case line := <-sentLines:
			if strings.HasPrefix(line, "CAP END") {
				capEndCount++
			}
		case <-deadline:
			break drain
		}
	}
	if capEndCount != 1 {
		t.Fatalf("double handleSASLSuccess must send CAP END exactly once, got %d", capEndCount)
	}
}

// TestSASLEnabledButNotOfferedAborts verifies that when SASL is required but
// the server's CAP LS list does not include "sasl", the client aborts with QUIT
// (via handleSASLFailure) and never sends CAP END.
func TestSASLEnabledButNotOfferedAborts(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true

	// Call with a CAP list that does NOT include "sasl".
	c.handleCapLSMissingSASL("multi-prefix server-time")

	if !c.AuthFailed() {
		t.Fatal("SASL enabled but not offered must mark AuthFailed")
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
			t.Fatal("timed out waiting for QUIT after SASL not offered")
		}
	}
	if sawCapEnd {
		t.Fatal("must not send CAP END when required SASL is unavailable")
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

// TestPLAINAuthServerAbortAbortsConnection verifies that when the server sends
// AUTHENTICATE * during PLAIN auth, the client sets AuthFailed and sends QUIT
// instead of CAP END.
func TestPLAINAuthServerAbortAbortsConnection(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true

	assertSASLAbortBehavior(t, c, sentLines, func() {
		c.handlePLAINAuth("*")
	})
}

// TestEXTERNALAuthServerAbortAbortsConnection verifies that when the server sends
// AUTHENTICATE * during EXTERNAL auth, the client sets AuthFailed and sends QUIT
// instead of CAP END.
func TestEXTERNALAuthServerAbortAbortsConnection(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true

	assertSASLAbortBehavior(t, c, sentLines, func() {
		c.handleEXTERNALAuth("*")
	})
}

// TestSCRAMServerAbortAbortsConnection verifies that when the server sends
// AUTHENTICATE * during SCRAM auth (server-side abort), the client sets
// AuthFailed and sends QUIT instead of CAP END.
func TestSCRAMServerAbortAbortsConnection(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true
	c.saslMechanism = "SCRAM-SHA-256"

	assertSASLAbortBehavior(t, c, sentLines, func() {
		c.handleSCRAMAuth("*")
	})
}

// TestAbortSASLClientSideErrorAbortsConnection verifies that a client-detected
// SCRAM protocol error (e.g. invalid base64) routes through handleSASLFailure:
// AuthFailed() must be set, QUIT must be sent, and CAP END must never appear.
// abortSASL also sends AUTHENTICATE * as a protocol-level abort signal before
// tearing down; we accept that in the sent lines.
func TestAbortSASLClientSideErrorAbortsConnection(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true
	c.saslMechanism = "SCRAM-SHA-256"

	assertSASLAbortBehavior(t, c, sentLines, func() {
		c.abortSASL("decode error")
	})
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
