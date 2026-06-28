package irc

import (
	"strings"
	"testing"
	"time"
)

// A 901 RPL_LOGGEDOUT that arrives AFTER the SASL handshake has completed is not
// an authentication failure — it is a benign account-state change. Libera's
// services emit it during a NickServ REGAIN as they re-bind the session's
// account. Treating it as a failure aborts a perfectly good connection, which
// forces a reconnect that wipes every channel roster. This is the regression
// guard for that bug: a post-auth 901 must NOT tear the connection down.
func TestLoggedOutAfterAuthDoesNotAbort(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true

	// We already authenticated successfully and finished the handshake earlier.
	c.mu.Lock()
	c.saslAuthenticated = true
	c.saslInProgress = false
	c.mu.Unlock()

	// A 901 arrives now (e.g. during a REGAIN), long after registration.
	c.handleLoggedOut()

	if c.AuthFailed() {
		t.Fatal("a post-registration 901 must not be recorded as an auth failure")
	}

	// No QUIT may be sent: the connection must stay up. Drain until idle.
	idle := time.After(200 * time.Millisecond)
	for {
		select {
		case line := <-sentLines:
			if strings.HasPrefix(line, "QUIT") {
				t.Fatalf("post-auth 901 aborted the connection: %q", line)
			}
		case <-idle:
			return // nothing aborting the connection — good
		}
	}
}

// A 901 RPL_LOGGEDOUT received DURING the SASL handshake is a real auth failure
// and must still abort the connection (the original, correct behavior).
func TestLoggedOutDuringAuthAborts(t *testing.T) {
	c := newAnnounceBotTestClient(t)
	conn, sentLines := newConnectedPipe(t)
	c.conn = conn
	c.saslEnabled = true

	c.mu.Lock()
	c.saslInProgress = true
	c.mu.Unlock()

	c.handleLoggedOut()

	if !c.AuthFailed() {
		t.Fatal("a 901 during the SASL handshake must record an auth failure")
	}
	drainUntilPrefix(t, sentLines, "QUIT", time.Second)
}
