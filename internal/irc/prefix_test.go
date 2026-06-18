package irc

import "testing"

func TestSplitMembershipPrefixes(t *testing.T) {
	tests := []struct {
		name          string
		entry         string
		validPrefixes string
		wantModes     string
		wantNick      string
	}{
		{
			name:          "no prefix",
			entry:         "nick",
			validPrefixes: standardMembershipPrefixes,
			wantModes:     "",
			wantNick:      "nick",
		},
		{
			name:          "single op prefix",
			entry:         "@nick",
			validPrefixes: standardMembershipPrefixes,
			wantModes:     "@",
			wantNick:      "nick",
		},
		{
			name:          "multi-prefix op and voice",
			entry:         "@+nick",
			validPrefixes: standardMembershipPrefixes,
			wantModes:     "@+",
			wantNick:      "nick",
		},
		{
			name:          "full prefix stack",
			entry:         "~&@%+nick",
			validPrefixes: standardMembershipPrefixes,
			wantModes:     "~&@%+",
			wantNick:      "nick",
		},
		{
			name:          "order preserved as received",
			entry:         "+@nick",
			validPrefixes: standardMembershipPrefixes,
			wantModes:     "+@",
			wantNick:      "nick",
		},
		{
			name:          "dynamic server prefix set from ISUPPORT",
			entry:         "@+nick",
			validPrefixes: "@+", // server advertised only PREFIX=(ov)@+
			wantModes:     "@+",
			wantNick:      "nick",
		},
		{
			name:          "char not in server set is treated as part of nick",
			entry:         "~nick", // server doesn't advertise '~'
			validPrefixes: "@+",
			wantModes:     "",
			wantNick:      "~nick",
		},
		{
			name:          "empty valid set leaves entry untouched",
			entry:         "@nick",
			validPrefixes: "",
			wantModes:     "",
			wantNick:      "@nick",
		},
		{
			name:          "empty entry",
			entry:         "",
			validPrefixes: standardMembershipPrefixes,
			wantModes:     "",
			wantNick:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modes, nick := splitMembershipPrefixes(tt.entry, tt.validPrefixes)
			if modes != tt.wantModes {
				t.Errorf("modes = %q, want %q", modes, tt.wantModes)
			}
			if nick != tt.wantNick {
				t.Errorf("nick = %q, want %q", nick, tt.wantNick)
			}
		})
	}
}
