package irc

import (
	"testing"
)

func TestParseExtban(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantPrefix rune
		wantTypes  []rune
		wantOK     bool
	}{
		{
			name:       "prefix and types",
			value:      "$,ajrxc",
			wantPrefix: '$',
			wantTypes:  []rune{'a', 'c', 'j', 'r', 'x'},
			wantOK:     true,
		},
		{
			name:       "tilde prefix",
			value:      "~,qjncrRa",
			wantPrefix: '~',
			wantTypes:  []rune{'R', 'a', 'c', 'j', 'n', 'q', 'r'},
			wantOK:     true,
		},
		{
			name:       "no prefix, types only",
			value:      ",a",
			wantPrefix: 0,
			wantTypes:  []rune{'a'},
			wantOK:     true,
		},
		{
			name:   "empty value",
			value:  "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, types, ok := parseExtban(tt.value)
			if ok != tt.wantOK {
				t.Fatalf("parseExtban(%q) ok = %v, want %v", tt.value, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if prefix != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
			if got := sortedRunes(types); got != string(tt.wantTypes) {
				t.Errorf("types = %q, want %q", got, string(tt.wantTypes))
			}
		})
	}
}

// supportsAccountExtban reports whether the parsed EXTBAN advertises the account
// type 'a' — the ratified account-extban feature.
func TestSupportsAccountExtban(t *testing.T) {
	if _, types, _ := parseExtban("$,ajrxc"); !types['a'] {
		t.Error("expected account extban type 'a' to be present")
	}
	if _, types, _ := parseExtban("$,jrxc"); types['a'] {
		t.Error("did not expect account extban type 'a'")
	}
}
