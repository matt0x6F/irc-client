package irc

import (
	"testing"

	"github.com/ergochat/irc-go/ircmsg"
)

// TestSoftwareFamilyFromRegistration exercises the full wiring a live connection would:
// the RPL_MYINFO (004) callback captures the version, ISUPPORT supplies EXTBAN, and
// SoftwareFamily() resolves the family — no real server required.
func TestSoftwareFamilyFromRegistration(t *testing.T) {
	t.Run("004 version drives detection", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyMyInfo(ircmsg.Message{
			Params: []string{"mynick", "irc.libera.chat", "solanum-1.0-dev", "DGRWdgilnstu", "CFLMPQcgimnprstuz"},
		})
		c.applyISUPPORTToken("EXTBAN=$,ajrxc")
		if got := c.SoftwareFamily(); got != "solanum" {
			t.Errorf("SoftwareFamily() = %q, want %q", got, "solanum")
		}
	})

	t.Run("blank version falls back to EXTBAN fingerprint", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		// No 004 received; only the "$" extban fingerprint is available.
		c.applyISUPPORTToken("EXTBAN=$,ajrxc")
		if got := c.SoftwareFamily(); got != "solanum" {
			t.Errorf("SoftwareFamily() = %q, want %q (fingerprint)", got, "solanum")
		}
	})

	t.Run("unknown server stays empty", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyMyInfo(ircmsg.Message{Params: []string{"mynick", "irc.example.org", "MysteryIRCd-9"}})
		if got := c.SoftwareFamily(); got != "" {
			t.Errorf("SoftwareFamily() = %q, want empty", got)
		}
	})

	t.Run("short 004 is ignored", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyMyInfo(ircmsg.Message{Params: []string{"mynick", "irc.example.org"}})
		if got := c.SoftwareFamily(); got != "" {
			t.Errorf("SoftwareFamily() = %q, want empty", got)
		}
	})
}

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
