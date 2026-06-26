package irc

import (
	"testing"
)

func TestDetectServerFamily(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		extbanPrefix rune
		want         string
	}{
		// Primary signal: the RPL_MYINFO (004) version token.
		{"solanum", "solanum-1.0-dev", '$', "solanum"},
		{"charybdis maps to solanum", "charybdis-4.1.2", '$', "solanum"},
		{"ircd-seven maps to solanum", "ircd-seven-1.1.9", '$', "solanum"},
		{"ratbox maps to solanum", "ratbox-3.0.10", '$', "solanum"},
		{"unrealircd", "UnrealIRCd-6.1.0", '~', "unrealircd"},
		{"inspircd", "InspIRCd-4", '~', "inspircd"},
		{"ergo", "ergo-2.13.0", '~', "ergo"},
		{"oragono maps to ergo", "oragono-2.6.1", '~', "ergo"},
		{"case insensitive", "SOLANUM-1.0", '$', "solanum"},

		// Fallback fingerprint when the version is blank/unrecognized: a "$" EXTBAN
		// prefix uniquely marks the charybdis/solanum family.
		{"blank version, $ extban fingerprints solanum", "", '$', "solanum"},
		{"unknown version, $ extban fingerprints solanum", "MyCustomIRCd", '$', "solanum"},

		// "~" extban is shared by unreal and inspircd, so the fingerprint must NOT guess.
		{"blank version, ~ extban is ambiguous", "", '~', ""},
		{"unknown version, no extban", "SomeWeirdServer-9", 0, ""},
		{"all empty", "", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectServerFamily(tt.version, tt.extbanPrefix)
			if got != tt.want {
				t.Errorf("detectServerFamily(%q, %q) = %q, want %q",
					tt.version, string(tt.extbanPrefix), got, tt.want)
			}
		})
	}
}
