package irc

import "testing"

func TestSetnameUpdatesRealname(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleSetname(parse(t, ":alice!a@h SETNAME :Alice Liddell"))
	if meta, _ := c.UserMetaFor("alice"); meta.Realname != "Alice Liddell" {
		t.Fatalf("after SETNAME: %+v", meta)
	}

	// A later SETNAME replaces the realname.
	c.handleSetname(parse(t, ":alice!a@h SETNAME :Alice B."))
	if meta, _ := c.UserMetaFor("alice"); meta.Realname != "Alice B." {
		t.Fatalf("after second SETNAME: %+v", meta)
	}
}

func TestSetnameIgnoredWithoutParams(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleSetname(parse(t, ":bob!b@h SETNAME"))
	if _, ok := c.UserMetaFor("bob"); ok {
		t.Fatalf("SETNAME with no params created a roster entry")
	}
}

func TestExtendedJoinPopulatesRealname(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.enabledCaps["extended-join"] = true

	// extended-join carries account (2nd param) and realname (3rd, trailing).
	c.maybeApplyExtendedJoin(parse(t, ":carol!c@h JOIN #chan carol_acct :Carol Danvers"))
	meta, _ := c.UserMetaFor("carol")
	if meta.Account != "carol_acct" || meta.Realname != "Carol Danvers" {
		t.Fatalf("after extended JOIN: %+v", meta)
	}
}
