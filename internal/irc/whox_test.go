package irc

import "testing"

// 354 field order for our "%tcuhnfar" query:
//   [ourNick, token, channel, user, host, nick, flags, account, realname]

func TestWhoxReplySeedsRoster(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleWhoxReply(parse(t, ":srv 354 me 332 #chan ~ident host.example alice H acct :Alice Liddell"))

	meta, ok := c.UserMetaFor("alice")
	if !ok {
		t.Fatal("expected roster entry for alice")
	}
	if meta.Away {
		t.Errorf("flags H should not be away: %+v", meta)
	}
	if meta.Account != "acct" {
		t.Errorf("account = %q, want acct", meta.Account)
	}
	if meta.Host != "~ident@host.example" {
		t.Errorf("host = %q, want ~ident@host.example", meta.Host)
	}
	if meta.Realname != "Alice Liddell" {
		t.Errorf("realname = %q, want Alice Liddell", meta.Realname)
	}
}

func TestWhoxReplyAwayAndNoAccount(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// flags "G*@" → gone/away; account "0" → not logged in.
	c.handleWhoxReply(parse(t, ":srv 354 me 332 #chan u h bob G*@ 0 :Bob"))

	meta, _ := c.UserMetaFor("bob")
	if !meta.Away {
		t.Errorf("flags G should be away: %+v", meta)
	}
	if meta.Account != "" {
		t.Errorf("account %q should be empty for '0'", meta.Account)
	}
}

func TestWhoxReplyIgnoresForeignToken(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// A user-initiated WHO carries a different (or no) token; must be ignored.
	c.handleWhoxReply(parse(t, ":srv 354 me 999 #chan u h carol H acct :Carol"))
	if _, ok := c.UserMetaFor("carol"); ok {
		t.Fatal("354 with a non-roster token must not touch the roster")
	}

	// Too few fields (e.g. a partial WHOX field set) is ignored, not a panic.
	c.handleWhoxReply(parse(t, ":srv 354 me 332 #chan"))
	if _, ok := c.UserMetaFor("nobody"); ok {
		t.Fatal("short 354 must not create entries")
	}
}
