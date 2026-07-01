package main

import "testing"

func TestAuthFailureBlocksReconnect(t *testing.T) {
	// Default / unset => block reconnect.
	if !authFailureBlocksReconnect("") {
		t.Error("unset setting must block reconnect after auth failure")
	}
	if !authFailureBlocksReconnect("false") {
		t.Error("false setting must block reconnect after auth failure")
	}
	// Opted in => allow reconnect.
	if authFailureBlocksReconnect("true") {
		t.Error("true setting must allow reconnect after auth failure")
	}
}
