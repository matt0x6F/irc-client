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

// TestRequestedCapsIncludesCapNotify: cap-notify must be in the wanted set so we
// request it explicitly (it's also implicit with CAP LS 302) and the server's ACK
// surfaces it in enabledCaps.
func TestRequestedCapsIncludesCapNotify(t *testing.T) {
	found := false
	for _, c := range requestedCaps {
		if c == "cap-notify" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("requestedCaps must include cap-notify; got %v", requestedCaps)
	}
}

// TestCapsToRequestFilters covers the shared offered∩wanted filter used by both the
// CAP LS and CAP NEW paths: only wanted caps that are offered and not already enabled
// are requested, sasl is gated on includeSASL, and order follows requestedCaps.
func TestCapsToRequestFilters(t *testing.T) {
	tests := []struct {
		name        string
		offered     string
		enabled     map[string]bool
		includeSASL bool
		want        []string
	}{
		{
			name:        "initial handshake requests offered wanted caps incl sasl",
			offered:     "sasl server-time echo-message unknown-cap",
			enabled:     map[string]bool{},
			includeSASL: true,
			want:        []string{"sasl", "server-time", "echo-message"},
		},
		{
			name:        "sasl excluded when includeSASL is false (CAP NEW)",
			offered:     "sasl server-time",
			enabled:     map[string]bool{},
			includeSASL: false,
			want:        []string{"server-time"},
		},
		{
			name:        "already-enabled caps are not re-requested",
			offered:     "server-time echo-message message-tags",
			enabled:     map[string]bool{"server-time": true, "echo-message": true},
			includeSASL: false,
			want:        []string{"message-tags"},
		},
		{
			name:        "nothing offered yields empty",
			offered:     "unknown-cap another-unknown",
			enabled:     map[string]bool{},
			includeSASL: true,
			want:        nil,
		},
		{
			name:        "values on offered caps are matched by name (chathistory=50)",
			offered:     "draft/chathistory=50 batch",
			enabled:     map[string]bool{},
			includeSASL: false,
			want:        []string{"batch", "draft/chathistory"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capsToRequest(tt.offered, tt.enabled, tt.includeSASL)
			if len(got) != len(tt.want) {
				t.Fatalf("capsToRequest(%q) = %v; want %v", tt.offered, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("capsToRequest(%q)[%d] = %q; want %q", tt.offered, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestCapDelNameStripping verifies the defensive name-stripping the CAP DEL handler
// relies on: CAP DEL lists bare names, but a stray "cap=value" must still resolve to
// the bare capability name so the right key is deleted from enabledCaps.
func TestCapDelNameStripping(t *testing.T) {
	enabled := map[string]bool{"server-time": true, "echo-message": true, "message-tags": true}
	// Mirror the handler's parse: split fields, strip any "=value" suffix, delete.
	for _, capName := range strings.Fields("server-time echo-message=ignored") {
		if idx := strings.Index(capName, "="); idx != -1 {
			capName = capName[:idx]
		}
		delete(enabled, capName)
	}
	if enabled["server-time"] {
		t.Error("server-time should have been removed")
	}
	if enabled["echo-message"] {
		t.Error("echo-message (with stray =value) should have been removed by name")
	}
	if !enabled["message-tags"] {
		t.Error("message-tags was not in the DEL list and must remain")
	}
}
