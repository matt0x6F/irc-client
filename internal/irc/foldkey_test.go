package irc

import "testing"

// setCaseMapping overrides the atomic CASEMAPPING the client would otherwise
// learn from RPL_ISUPPORT, so a test can pin the fold rule.
func setCaseMapping(c *IRCClient, mapping string) {
	c.caseMappingAtomic.Store(&mapping)
}

// TestFoldKeyUsesRFC1459ForRosterKeys proves in-memory roster keys fold under the
// server's CASEMAPPING, not plain ASCII lowercasing. On an rfc1459 network the
// bytes []\~ are the same letter as {}|^, so "Nick[a]" and "nick{a}" are the same
// user and must share one userMeta bucket. Plain strings.ToLower would split them.
func TestFoldKeyUsesRFC1459ForRosterKeys(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	setCaseMapping(c, "rfc1459")

	c.applyUserMeta("Nick[a]", func(m *UserMeta) { m.Account = "acct" })

	// The rfc1459-equivalent nick must resolve to the same entry.
	if meta, ok := c.UserMetaFor("nick{a}"); !ok || meta.Account != "acct" {
		t.Fatalf("rfc1459 fold missed: Nick[a] and nick{a} landed in different buckets (ok=%v meta=%+v)", ok, meta)
	}
}

// TestFoldKeyRespectsAsciiMapping proves the fold honors the advertised mapping:
// under "ascii" the bracket characters are NOT equivalent, so "Nick[a]" and
// "nick{a}" are distinct users — while ASCII case still folds ("NICK[A]" hits).
func TestFoldKeyRespectsAsciiMapping(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	setCaseMapping(c, "ascii")

	c.applyUserMeta("Nick[a]", func(m *UserMeta) { m.Account = "acct" })

	if _, ok := c.UserMetaFor("nick{a}"); ok {
		t.Fatalf("ascii mapping must not fold brackets: nick{a} wrongly matched Nick[a]")
	}
	if meta, ok := c.UserMetaFor("NICK[A]"); !ok || meta.Account != "acct" {
		t.Fatalf("ascii mapping must still fold A-Z: NICK[A] should match Nick[a] (ok=%v meta=%+v)", ok, meta)
	}
}
