package irc

import "testing"

func TestSplitNickUserHost(t *testing.T) {
	tests := []struct {
		name         string
		entry        string
		wantNick     string
		wantUserHost string
	}{
		{"bare nick (no cap)", "nick", "nick", ""},
		{"nick with user@host", "nick!user@host", "nick", "user@host"},
		{"ident with leading tilde", "alice!~alice@example.com", "alice", "~alice@example.com"},
		{"trailing bang only", "nick!", "nick", ""},
		{"host with embedded at", "bob!b@a@weird.host", "bob", "b@a@weird.host"},
		{"empty entry", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nick, uh := splitNickUserHost(tt.entry)
			if nick != tt.wantNick || uh != tt.wantUserHost {
				t.Errorf("splitNickUserHost(%q) = (%q, %q), want (%q, %q)", tt.entry, nick, uh, tt.wantNick, tt.wantUserHost)
			}
		})
	}
}

// TestNamesEntryPrefixAndUserHost verifies the two parsers chain the way the 353
// handler uses them: peel membership prefixes first, then split the bare nick
// from its user@host (present only when userhost-in-names is negotiated).
func TestNamesEntryPrefixAndUserHost(t *testing.T) {
	tests := []struct {
		name         string
		entry        string
		wantModes    string
		wantNick     string
		wantUserHost string
	}{
		{"plain nick", "nick", "", "nick", ""},
		{"op with userhost", "@alice!~alice@host", "@", "alice", "~alice@host"},
		{"multi-prefix with userhost", "@+bob!b@host", "@+", "bob", "b@host"},
		{"userhost only, no prefix", "carol!c@host", "", "carol", "c@host"},
		{"op, no userhost (plain NAMES)", "@dave", "@", "dave", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modes, rest := splitMembershipPrefixes(tt.entry, standardMembershipPrefixes)
			nick, uh := splitNickUserHost(rest)
			if modes != tt.wantModes || nick != tt.wantNick || uh != tt.wantUserHost {
				t.Errorf("parse(%q) = modes %q, nick %q, userhost %q; want %q / %q / %q",
					tt.entry, modes, nick, uh, tt.wantModes, tt.wantNick, tt.wantUserHost)
			}
		})
	}
}
