package ircevent

import (
	"testing"

	"github.com/ergochat/irc-go/ircmsg"
)

// The handshake requests caps as a single combined `CAP REQ` line containing only
// the caps the server advertised (IRCv3 CAP REQ is atomic, and one-per-line trips
// server anti-flood throttling). These tests cover the two new pieces of logic that
// make that safe: filtering wanted caps against the advertisement, and detecting
// when the (possibly multiline) CAP LS advertisement is complete.

func TestCapsToRequestFiltersToAdvertised(t *testing.T) {
	irc := &Connection{
		RequestCaps: []string{"sasl", "server-time", "draft/chathistory", "message-tags"},
		capsAdvertised: map[string]string{
			"server-time":  "",
			"message-tags": "",
			"sasl":         "PLAIN,EXTERNAL",
			"account-tag":  "", // advertised but not wanted
		},
	}

	got := irc.capsToRequest()

	// Only wanted-and-advertised caps, in RequestCaps preference order.
	want := []string{"sasl", "server-time", "message-tags"}
	if len(got) != len(want) {
		t.Fatalf("capsToRequest() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("capsToRequest() = %v, want %v (order matters)", got, want)
		}
	}
}

func TestCapsToRequestEmptyWhenNothingAdvertised(t *testing.T) {
	irc := &Connection{
		RequestCaps:    []string{"sasl", "server-time"},
		capsAdvertised: map[string]string{"multi-prefix": ""},
	}
	if got := irc.capsToRequest(); len(got) != 0 {
		t.Fatalf("capsToRequest() = %v, want empty", got)
	}
}

// handleCAP dispatches to handleCAPLS. Feeding a multiline CAP LS 302 advertisement
// must accumulate every line's caps and only signal completion (close capsLSChan)
// on the final, non-continuation line.
func TestCapLSMultilineSignalsOnlyOnFinalLine(t *testing.T) {
	irc := &Connection{
		RequestCaps: []string{"sasl", "server-time", "batch"},
		capsLSChan:  make(chan empty),
	}

	// Non-final line carries a lone "*" before the cap list.
	irc.handleCAP(ircmsg.MakeMessage(nil, "server.name", "CAP", "*", "LS", "*", "server-time batch"))

	select {
	case <-irc.capsLSChan:
		t.Fatal("capsLSChan closed on a continuation line; should wait for the final line")
	default:
	}
	if _, ok := irc.capsAdvertised["server-time"]; !ok {
		t.Fatal("continuation line caps were not recorded")
	}

	// Final line: no "*" continuation marker.
	irc.handleCAP(ircmsg.MakeMessage(nil, "server.name", "CAP", "*", "LS", "sasl=PLAIN"))

	select {
	case <-irc.capsLSChan:
	default:
		t.Fatal("capsLSChan not closed after the final CAP LS line")
	}

	got := irc.capsToRequest()
	want := []string{"sasl", "server-time", "batch"}
	if len(got) != len(want) {
		t.Fatalf("capsToRequest() after full LS = %v, want %v", got, want)
	}
}
