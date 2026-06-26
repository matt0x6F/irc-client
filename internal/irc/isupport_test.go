package irc

import "testing"

// TestApplyISUPPORTTokenExisting locks in the existing token dispatch behavior
// (PREFIX / CHANMODES / WHOX / MONITOR) after extraction into applyISUPPORTToken.
func TestApplyISUPPORTTokenExisting(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.applyISUPPORTToken("PREFIX=(ov)@+")
	c.applyISUPPORTToken("CHANMODES=beI,k,l,imnpst")
	c.applyISUPPORTToken("WHOX")
	c.applyISUPPORTToken("MONITOR=100")

	if !c.whoxSupported() {
		t.Error("WHOX token should set supportsWHOX")
	}
	caps := c.GetServerCapabilities()
	if caps.PrefixString != "(ov)@+" {
		t.Errorf("PrefixString = %q, want %q", caps.PrefixString, "(ov)@+")
	}
	if caps.ChanModes != "beI,k,l,imnpst" {
		t.Errorf("ChanModes = %q", caps.ChanModes)
	}
	c.mu.RLock()
	mon, lim := c.supportsMonitor, c.monitorLimit
	c.mu.RUnlock()
	if !mon || lim != 100 {
		t.Errorf("MONITOR: supports=%v limit=%d, want true/100", mon, lim)
	}
}

func TestApplyISUPPORTTokenUTF8Only(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	if c.GetServerCapabilities().UTF8Only {
		t.Fatal("UTF8Only should default to false")
	}
	c.applyISUPPORTToken("UTF8ONLY")
	if !c.GetServerCapabilities().UTF8Only {
		t.Error("UTF8ONLY token should set UTF8Only=true")
	}
}

func TestApplyISUPPORTTokenExtban(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.applyISUPPORTToken("EXTBAN=$,ajrxc")
	caps := c.GetServerCapabilities()
	if caps.ExtbanPrefix != '$' {
		t.Errorf("ExtbanPrefix = %q, want '$'", caps.ExtbanPrefix)
	}
	if !caps.ExtbanTypes['a'] {
		t.Error("expected account extban type 'a'")
	}
}

// TestExtbanInfo: the frontend-facing getter returns the prefix as a string and
// the type letters sorted, so the UI can test for 'a' and build "$a:..." masks.
func TestExtbanInfo(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	if prefix, types := c.ExtbanInfo(); prefix != "" || types != "" {
		t.Errorf("unadvertised: prefix=%q types=%q, want empty", prefix, types)
	}

	c.applyISUPPORTToken("EXTBAN=$,ajrxc")
	prefix, types := c.ExtbanInfo()
	if prefix != "$" {
		t.Errorf("prefix = %q, want %q", prefix, "$")
	}
	if types != "acjrx" {
		t.Errorf("types = %q, want %q (sorted)", types, "acjrx")
	}
}

func TestApplyISUPPORTToken_BotMode(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.applyISUPPORTToken("BOT=B")
	if got := c.BotMode(); got != "B" {
		t.Fatalf("BotMode() = %q, want %q", got, "B")
	}
	if c.serverCapabilities.BotModeChar != 'B' {
		t.Fatalf("BotModeChar = %q, want 'B'", c.serverCapabilities.BotModeChar)
	}
}

func TestApplyISUPPORTToken_BotMode_LowercaseLetter(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.applyISUPPORTToken("BOT=b")
	if got := c.BotMode(); got != "b" {
		t.Fatalf("BotMode() = %q, want %q", got, "b")
	}
}

func TestBotMode_Unadvertised(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	if got := c.BotMode(); got != "" {
		t.Fatalf("BotMode() = %q, want empty", got)
	}
}

func TestApplyISUPPORTToken_BotMode_Malformed(t *testing.T) {
	t.Run("empty value", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyISUPPORTToken("BOT=")
		if got := c.BotMode(); got != "" {
			t.Fatalf("BotMode() = %q, want empty for BOT=", got)
		}
	})
	t.Run("two-letter value", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyISUPPORTToken("BOT=AB")
		if got := c.BotMode(); got != "" {
			t.Fatalf("BotMode() = %q, want empty for BOT=AB", got)
		}
	})
}
