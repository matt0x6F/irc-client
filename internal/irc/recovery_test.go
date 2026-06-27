package irc

import "testing"

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
