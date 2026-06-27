package irc

import (
	"strings"
	"testing"
	"time"
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
