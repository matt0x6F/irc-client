package irc

import "testing"

// TestNamesOnSelfJoin: the nick list is built solely from the post-JOIN NAMES
// (353/366). With no-implicit-names the server suppresses that automatic reply, so
// on our own JOIN we must request NAMES explicitly or the roster goes empty.
// Without the cap, we rely on the implicit reply and send nothing.
func TestNamesOnSelfJoin(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	if got := c.namesOnSelfJoin("#chan"); got != "" {
		t.Errorf("without no-implicit-names: got %q, want empty (server sends NAMES implicitly)", got)
	}

	c.enabledCaps["no-implicit-names"] = true
	if got := c.namesOnSelfJoin("#chan"); got != "NAMES #chan" {
		t.Errorf("with no-implicit-names: got %q, want %q", got, "NAMES #chan")
	}
}
