package irc

import (
	"testing"
	"time"
)

func TestMatchHighlight(t *testing.T) {
	cases := []struct {
		nick, text string
		want       bool
	}{
		{"matt", "hey matt!", true},
		{"matt", "hi Matt", true},            // case-insensitive
		{"matt", "mattress is comfy", false}, // substring, not a word
		{"matt", "", false},
		{"matt", "email matt@example", true}, // '@' is a boundary
		{"", "anything", false},              // empty nick never matches
		{"a.b", "ping a.b now", true},        // regex-special chars are literal
		{"a.b", "ping axb now", false},       // '.' must not act as wildcard
	}
	for _, tc := range cases {
		if got := matchHighlight(tc.text, tc.nick); got != tc.want {
			t.Errorf("matchHighlight(%q, %q) = %v, want %v", tc.text, tc.nick, got, tc.want)
		}
	}
}

func TestMatchKeyword(t *testing.T) {
	kws := []string{"deploy", "prod"}
	if kw, ok := matchKeyword("deploy to prod", kws); !ok || kw != "deploy" {
		t.Errorf(`want ("deploy", true), got (%q, %v)`, kw, ok)
	}
	if kw, ok := matchKeyword("DEPLOY now", kws); !ok || kw != "deploy" {
		t.Errorf("keyword match should be case-insensitive, got (%q, %v)", kw, ok)
	}
	if _, ok := matchKeyword("deployment pipeline", kws); ok {
		t.Error("word-boundary should reject 'deployment'")
	}
	if _, ok := matchKeyword("nothing here", nil); ok {
		t.Error("empty keyword list should never match")
	}
}

func TestClassifyMessageActivity(t *testing.T) {
	all := ActivityConfig{Highlights: true, Keywords: true, Invites: true, PMs: true, Notices: true, Privmsgs: true, KeywordList: []string{"deploy"}}

	if src, _, ok := ClassifyMessageActivity(all, "matt", "matt", "bob", "hi", "privmsg", true); !ok || src != ActivityPM {
		t.Errorf("PM should classify as pm, got (%v, %v)", src, ok)
	}
	if _, _, ok := ClassifyMessageActivity(ActivityConfig{PMs: false, Notices: true, Privmsgs: true}, "matt", "matt", "bob", "hi", "privmsg", true); ok {
		t.Error("PM source disabled should not match")
	}
	if src, _, ok := ClassifyMessageActivity(all, "matt", "#dev", "bob", "hey matt", "privmsg", false); !ok || src != ActivityHighlight {
		t.Errorf("nick mention should classify as highlight, got (%v, %v)", src, ok)
	}
	if src, kw, ok := ClassifyMessageActivity(all, "matt", "#dev", "bob", "deploy is green", "privmsg", false); !ok || src != ActivityKeyword || kw != "deploy" {
		t.Errorf("keyword should classify as keyword, got (%v, %q, %v)", src, kw, ok)
	}
	// highlight wins over keyword when both present
	if src, _, _ := ClassifyMessageActivity(all, "matt", "#dev", "bob", "matt deploy now", "privmsg", false); src != ActivityHighlight {
		t.Errorf("highlight should take precedence, got %v", src)
	}
	if _, _, ok := ClassifyMessageActivity(all, "matt", "#dev", "bob", "just chatter", "privmsg", false); ok {
		t.Error("plain chatter should not match")
	}
}

func TestClassifyMessageActivity_TypeVeto(t *testing.T) {
	base := ActivityConfig{Highlights: true, Keywords: true, Invites: true, PMs: true, Notices: true, Privmsgs: true}

	// Notices off: a service NOTICE PM produces nothing.
	cfg := base
	cfg.Notices = false
	if _, _, ok := ClassifyMessageActivity(cfg, "me", "", "ChanServ", "hello", "notice", true); ok {
		t.Fatalf("notice should be vetoed when Notices=false")
	}
	// ...but a PRIVMSG PM still classifies.
	if _, _, ok := ClassifyMessageActivity(cfg, "me", "", "alice", "hi", "privmsg", true); !ok {
		t.Fatalf("privmsg PM should classify when Privmsgs=true")
	}

	// Privmsgs off: a PRIVMSG PM produces nothing; a NOTICE PM still classifies.
	cfg = base
	cfg.Privmsgs = false
	if _, _, ok := ClassifyMessageActivity(cfg, "me", "", "alice", "hi", "privmsg", true); ok {
		t.Fatalf("privmsg should be vetoed when Privmsgs=false")
	}
	if _, _, ok := ClassifyMessageActivity(cfg, "me", "", "ChanServ", "hello", "notice", true); !ok {
		t.Fatalf("notice PM should classify when Notices=true")
	}

	// Both on: unchanged behavior — a channel highlight classifies.
	if src, _, ok := ClassifyMessageActivity(base, "me", "#chan", "alice", "hey me!", "privmsg", false); !ok || src != ActivityHighlight {
		t.Fatalf("highlight expected, got src=%v ok=%v", src, ok)
	}
}

func TestActivityItemFromMessage(t *testing.T) {
	ts := time.Now()
	pm := ActivityItemFromMessage(7, ActivityPM, "", "matt", "bob", "ping you around?", "mid1", true, ts)
	if pm.Target != "bob" || pm.Actor != "bob" || pm.SourceType != "pm" || pm.MsgID != "mid1" {
		t.Errorf("PM row mapped wrong: %+v", pm)
	}
	hl := ActivityItemFromMessage(7, ActivityHighlight, "", "#dev", "alice", "matt look", "mid2", false, ts)
	if hl.Target != "#dev" || hl.Actor != "alice" || hl.Keyword != "" {
		t.Errorf("highlight row mapped wrong: %+v", hl)
	}
	long := ActivityItemFromMessage(7, ActivityHighlight, "", "#dev", "alice", stringOfLen(300), "m", false, ts)
	if len([]rune(long.Preview)) > 141 {
		t.Errorf("preview should be truncated, got len %d", len([]rune(long.Preview)))
	}
}

func stringOfLen(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
