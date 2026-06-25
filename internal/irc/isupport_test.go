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
