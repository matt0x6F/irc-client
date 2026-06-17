package irc

import (
	"strings"
	"testing"
)

// TestAccumulateCapLSSingleLine: a one-line CAP LS completes immediately and
// yields exactly the advertised tokens.
func TestAccumulateCapLSSingleLine(t *testing.T) {
	var buf strings.Builder
	// :server CAP nick LS :sasl server-time message-tags
	got, complete := accumulateCapLS([]string{"nick", "LS", "sasl server-time message-tags"}, &buf)
	if !complete {
		t.Fatal("single-line LS should complete")
	}
	for _, want := range []string{"sasl", "server-time", "message-tags"} {
		if !contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}
}

// TestAccumulateCapLSMultiline is the regression for the CAP LS 302 continuation
// bug: a server (e.g. Ergo, Libera) splits its long capability list across lines,
// marking non-final lines with a "*". Capabilities advertised on the FIRST line
// (here batch + draft/chathistory) must survive into the assembled list — the old
// code read params[2] ("*") on continuation lines and dropped them, which silently
// disabled CHATHISTORY against any server that paginates CAP LS.
func TestAccumulateCapLSMultiline(t *testing.T) {
	var buf strings.Builder

	// Line 1 (continuation): :server CAP nick LS * :batch draft/chathistory cap-notify
	got, complete := accumulateCapLS([]string{"nick", "LS", "*", "batch draft/chathistory cap-notify"}, &buf)
	if complete {
		t.Fatal("continuation line (with *) must not complete the advertisement")
	}
	if got != "" {
		t.Errorf("continuation line should return no assembled list yet, got %q", got)
	}

	// Line 2 (final): :server CAP nick LS :echo-message message-tags server-time
	got, complete = accumulateCapLS([]string{"nick", "LS", "echo-message message-tags server-time"}, &buf)
	if !complete {
		t.Fatal("final line (no *) must complete the advertisement")
	}

	// The assembled list must contain caps from BOTH lines, and never the literal "*".
	for _, want := range []string{"batch", "draft/chathistory", "cap-notify", "echo-message", "message-tags", "server-time"} {
		if !contains(got, want) {
			t.Errorf("expected %q in assembled caps %q", want, got)
		}
	}
	if contains(got, "*") {
		t.Errorf("assembled caps must not contain the continuation marker: %q", got)
	}
	// chathistory limit values advertised on a continuation line must be parseable too.
	if v, ok := capValue("batch draft/chathistory=50 server-time", "draft/chathistory"); !ok || v != "50" {
		t.Errorf("capValue(draft/chathistory) = %q,%v; want 50,true", v, ok)
	}
}

// TestAccumulateCapLSShortLine: a malformed/short LS line is ignored, not panicked on.
func TestAccumulateCapLSShortLine(t *testing.T) {
	var buf strings.Builder
	if got, complete := accumulateCapLS([]string{"nick", "LS"}, &buf); complete || got != "" {
		t.Errorf("short LS line should be ignored, got %q,%v", got, complete)
	}
}
