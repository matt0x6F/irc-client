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
