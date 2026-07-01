package irc

import (
	"strings"
	"testing"
	"time"
)

// TestServerErrorNumericWritesStatus verifies that a server error numeric with
// no dedicated handler (here ERR_NOSUCHNICK / 401) is surfaced to the status
// buffer rather than dropped silently.
func TestServerErrorNumericWritesStatus(t *testing.T) {
	c := newNickTestClient(t)
	c.currentNick = "matt0x6f_0"

	c.handleServerNumeric(parseLine(t, ":server 401 matt0x6f_0 ghost :No such nick/channel"))

	msgs := statusMessages(t, c)
	var found bool
	for _, m := range msgs {
		if strings.Contains(m.Message, "ghost") && strings.Contains(m.Message, "No such nick/channel") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a status line for 401 mentioning the nick and reason; got: %v", msgs)
	}
}

func TestCasefold(t *testing.T) {
	cases := []struct {
		name    string
		mapping string
		in      string
		want    string
	}{
		{"ascii lowercases only A-Z", "ascii", "Hello[World]", "hello[world]"},
		{"ascii leaves brackets and tilde", "ascii", `Nick[A]\~`, `nick[a]\~`},
		{"rfc1459 folds brackets and tilde", "rfc1459", `Nick[A]\~`, "nick{a}|^"},
		{"rfc1459-strict folds brackets but not tilde", "rfc1459-strict", `Nick[A]\~`, "nick{a}|~"},
		{"strict-rfc1459 alias", "strict-rfc1459", "[]\\~", "{}|~"},
		{"empty mapping defaults to rfc1459", "", "ABC[~", "abc{^"},
		{"unknown mapping folds as ascii", "precis", `Nick[~`, `nick[~`},
		{"non-ascii passes through", "rfc1459", "ÄÖ", "ÄÖ"},
		{"case-insensitive mapping name", "RFC1459", "[A]", "{a}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := casefold(tc.mapping, tc.in); got != tc.want {
				t.Fatalf("casefold(%q, %q) = %q, want %q", tc.mapping, tc.in, got, tc.want)
			}
		})
	}
}

func TestChannelNameMatches(t *testing.T) {
	cases := []struct {
		name      string
		chantypes string
		in        string
		want      bool
	}{
		{"hash is a channel by default", "", "#chan", true},
		{"ampersand is a channel by default", "", "&local", true},
		{"plus is not a channel by default", "", "+modeless", false},
		{"plus is a channel when advertised", "#&+", "+modeless", true},
		{"bang safe channel when advertised", "#&!", "!safe", true},
		{"nick is not a channel", "#&", "someone", false},
		{"empty name is not a channel", "#&", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := channelNameMatches(tc.in, tc.chantypes); got != tc.want {
				t.Fatalf("channelNameMatches(%q, %q) = %v, want %v", tc.in, tc.chantypes, got, tc.want)
			}
		})
	}
}

func TestISUPPORTChanTypesDrivesChannelDetection(t *testing.T) {
	c := &IRCClient{serverCapabilities: &ServerCapabilities{}}
	// Default before any 005: # and & are channels, + is not.
	if !c.isChannelName("#chan") || c.isChannelName("+local") {
		t.Fatal("default CHANTYPES should be #&")
	}
	c.applyISUPPORTToken("CHANTYPES=#&+")
	if !c.isChannelName("+local") {
		t.Fatal("after CHANTYPES=#&+, + should be a channel prefix")
	}
	if got := c.ChanTypes(); got != "#&+" {
		t.Fatalf("ChanTypes() = %q, want #&+", got)
	}
}

func TestISUPPORTCasemappingDrivesNickFolding(t *testing.T) {
	c := &IRCClient{serverCapabilities: &ServerCapabilities{}}
	// Default (no 005) is rfc1459: []\~ fold to {}|^, so these are one nick.
	if !c.sameName("Nick[a]", "nick{a}") {
		t.Fatal("default rfc1459 should treat Nick[a] and nick{a} as the same nick")
	}
	c.applyISUPPORTToken("CASEMAPPING=ascii")
	if c.sameName("Nick[a]", "nick{a}") {
		t.Fatal("ascii casemapping must NOT fold brackets — these are distinct nicks")
	}
	if got := c.CaseMapping(); got != "ascii" {
		t.Fatalf("CaseMapping() = %q, want ascii", got)
	}
}

func TestExportedChanTypesAndCaseMapping(t *testing.T) {
	c := &IRCClient{serverCapabilities: &ServerCapabilities{}}
	// Defaults before any 005 (these are surfaced to the frontend).
	if got := c.ChanTypes(); got != "#&" {
		t.Fatalf("default ChanTypes() = %q, want #&", got)
	}
	if got := c.CaseMapping(); got != "rfc1459" {
		t.Fatalf("default CaseMapping() = %q, want rfc1459", got)
	}
	c.applyISUPPORTToken("CHANTYPES=#&+!")
	c.applyISUPPORTToken("CASEMAPPING=ascii")
	if got := c.ChanTypes(); got != "#&+!" {
		t.Fatalf("ChanTypes() = %q, want #&+!", got)
	}
	if got := c.CaseMapping(); got != "ascii" {
		t.Fatalf("CaseMapping() = %q, want ascii", got)
	}
}

func TestFormatTopicWhoTime(t *testing.T) {
	when := time.Date(2026, time.June, 30, 14, 22, 0, 0, time.UTC)
	if got, want := formatTopicWhoTime("alice", when), "Topic set by alice on Jun 30, 2026 14:22"; got != want {
		t.Fatalf("formatTopicWhoTime = %q, want %q", got, want)
	}
	// A missing setter still yields a sensible line.
	if got, want := formatTopicWhoTime("", when), "Topic set on Jun 30, 2026 14:22"; got != want {
		t.Fatalf("formatTopicWhoTime (no setter) = %q, want %q", got, want)
	}
}

func TestFormatServerNumeric(t *testing.T) {
	cases := []struct {
		name   string
		params []string
		want   string
	}{
		{"subject and description", []string{"me", "somenick", "No such nick/channel"}, "somenick: No such nick/channel"},
		{"unknown command", []string{"me", "WIBBLE", "Unknown command"}, "WIBBLE: Unknown command"},
		{"description only", []string{"me", "You're now an oper"}, "You're now an oper"},
		{"multiple subjects", []string{"me", "#chan", "extra", "Cannot send to channel"}, "#chan extra: Cannot send to channel"},
		{"only our nick yields nothing", []string{"me"}, ""},
		{"no params yields nothing", []string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatServerNumeric(tc.params); got != tc.want {
				t.Fatalf("formatServerNumeric(%v) = %q, want %q", tc.params, got, tc.want)
			}
		})
	}
}
