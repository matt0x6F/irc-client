package irc

import "testing"

// A raw command must never contain a CR or LF: the underlying library appends
// its own "\r\n" without validation, so an embedded newline would smuggle an
// additional IRC command onto the wire (CRLF injection).
func TestValidateRawCommandRejectsNewline(t *testing.T) {
	bad := map[string]string{
		"CRLF split":    "JOIN #chan\r\nJOIN #evil",
		"bare LF":       "NICK foo\nQUIT :bye",
		"bare CR":       "TOPIC #chan :a\rb",
		"trailing CRLF": "PRIVMSG #chan :hi\r\n",
	}
	for name, cmd := range bad {
		if err := validateRawCommand(cmd); err == nil {
			t.Errorf("%s: expected error for %q, got nil", name, cmd)
		}
	}

	good := []string{"JOIN #chan", "PRIVMSG #chan :hello world", "WHOIS someone"}
	for _, cmd := range good {
		if err := validateRawCommand(cmd); err != nil {
			t.Errorf("clean command %q rejected: %v", cmd, err)
		}
	}
}
