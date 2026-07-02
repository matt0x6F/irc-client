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

// TestApplyISUPPORTToken_LineLen: LINELEN=<n> (ergo extension) raises the line
// budget outbound splitting works against. Absent or malformed values leave the
// zero value so consumers fall back to the RFC 1459 512-byte default.
func TestApplyISUPPORTToken_LineLen(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	if got := c.GetServerCapabilities().LineLen; got != 0 {
		t.Fatalf("LineLen default = %d, want 0 (unadvertised)", got)
	}

	c.applyISUPPORTToken("LINELEN=1024")
	if got := c.GetServerCapabilities().LineLen; got != 1024 {
		t.Errorf("LineLen = %d, want 1024", got)
	}

	t.Run("malformed value ignored", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyISUPPORTToken("LINELEN=abc")
		if got := c.GetServerCapabilities().LineLen; got != 0 {
			t.Errorf("LineLen = %d after LINELEN=abc, want 0", got)
		}
	})
	t.Run("non-positive value ignored", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyISUPPORTToken("LINELEN=0")
		if got := c.GetServerCapabilities().LineLen; got != 0 {
			t.Errorf("LineLen = %d after LINELEN=0, want 0", got)
		}
	})
}

// TestApplyISUPPORTToken_Modes: MODES=<n> caps mode changes per MODE command;
// an empty value means "no limit" (recorded as -1). Zero means unadvertised.
func TestApplyISUPPORTToken_Modes(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	if got := c.GetServerCapabilities().Modes; got != 0 {
		t.Fatalf("Modes default = %d, want 0 (unadvertised)", got)
	}

	c.applyISUPPORTToken("MODES=4")
	if got := c.GetServerCapabilities().Modes; got != 4 {
		t.Errorf("Modes = %d, want 4", got)
	}

	t.Run("empty value means unlimited", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyISUPPORTToken("MODES=")
		if got := c.GetServerCapabilities().Modes; got != -1 {
			t.Errorf("Modes = %d after MODES=, want -1 (unlimited)", got)
		}
	})
	t.Run("malformed value ignored", func(t *testing.T) {
		c, _ := newUserMetaTestClient(t)
		c.applyISUPPORTToken("MODES=many")
		if got := c.GetServerCapabilities().Modes; got != 0 {
			t.Errorf("Modes = %d after MODES=many, want 0", got)
		}
	})
}

// TestApplyISUPPORTToken_StatusMsg: STATUSMSG=<prefixes> advertises the
// membership prefixes messages may be addressed to (e.g. "@#chan").
func TestApplyISUPPORTToken_StatusMsg(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	if got := c.GetServerCapabilities().StatusMsg; got != "" {
		t.Fatalf("StatusMsg default = %q, want empty", got)
	}
	c.applyISUPPORTToken("STATUSMSG=@+")
	if got := c.GetServerCapabilities().StatusMsg; got != "@+" {
		t.Errorf("StatusMsg = %q, want %q", got, "@+")
	}
}

// TestParseTargMax: TARGMAX=PRIVMSG:4,NOTICE:4,JOIN: — per-command target caps;
// an empty limit means "no limit" (recorded as -1); malformed entries skipped.
func TestParseTargMax(t *testing.T) {
	got := parseTargMax("PRIVMSG:4,NOTICE:4,JOIN:,bogus,WHOIS:x")
	want := map[string]int{"PRIVMSG": 4, "NOTICE": 4, "JOIN": -1}
	if len(got) != len(want) {
		t.Fatalf("parseTargMax = %v, want %v", got, want)
	}
	for cmd, limit := range want {
		if got[cmd] != limit {
			t.Errorf("parseTargMax[%q] = %d, want %d", cmd, got[cmd], limit)
		}
	}

	t.Run("lowercase command is folded to uppercase", func(t *testing.T) {
		got := parseTargMax("privmsg:2")
		if got["PRIVMSG"] != 2 {
			t.Errorf("parseTargMax[PRIVMSG] = %d, want 2", got["PRIVMSG"])
		}
	})
	t.Run("empty value yields empty map", func(t *testing.T) {
		if got := parseTargMax(""); len(got) != 0 {
			t.Errorf("parseTargMax(\"\") = %v, want empty", got)
		}
	})
}

func TestApplyISUPPORTToken_TargMax(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.applyISUPPORTToken("TARGMAX=PRIVMSG:4,JOIN:")
	caps := c.GetServerCapabilities()
	if caps.TargMax["PRIVMSG"] != 4 {
		t.Errorf("TargMax[PRIVMSG] = %d, want 4", caps.TargMax["PRIVMSG"])
	}
	if caps.TargMax["JOIN"] != -1 {
		t.Errorf("TargMax[JOIN] = %d, want -1 (unlimited)", caps.TargMax["JOIN"])
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
