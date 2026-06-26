package irc

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

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

// labeled-response path: the 354 rows arrive collected in a batch (no per-row
// token check) and are folded into the roster by handleWhoxBatch.
func TestWhoxBatchSeedsRosterFromItems(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	batch := &ircevent.Batch{
		Message: parse(t, ":srv BATCH +x labeled-response"),
		Items: []*ircevent.Batch{
			{Message: parse(t, ":srv 354 me 332 #chan ~u host.a alice H acctA :Alice")},
			{Message: parse(t, ":srv 354 me 332 #chan ~v host.b bob G*@ 0 :Bob")},
		},
	}
	c.handleWhoxBatch(batch)

	if m, ok := c.UserMetaFor("alice"); !ok || m.Account != "acctA" || m.Host != "~u@host.a" || m.Away {
		t.Fatalf("alice from batch: ok=%v %+v", ok, m)
	}
	if m, _ := c.UserMetaFor("bob"); !m.Away || m.Account != "" {
		t.Fatalf("bob from batch: %+v", m)
	}
}

func TestWhoxBatchNilSafe(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.handleWhoxBatch(nil) // must not panic
}

// newWhoxBotTestClient builds an IRCClient that can exercise both applyWhoxRow
// (needs userMeta + serverCapabilities) and markBot (needs knownBots + eventBus).
func newWhoxBotTestClient(t *testing.T) (*IRCClient, *botCounter) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewStorage(filepath.Join(dir, "test.db"), 100, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	net := &storage.Network{Name: "Libera", Address: "irc.libera.chat", Nickname: "matt0x6f", CreatedAt: time.Now()}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	bus := events.NewEventBus()
	counter := &botCounter{}
	bus.Subscribe(EventBotDetected, counter)

	c := &IRCClient{
		eventBus:           bus,
		storage:            s,
		networkID:          net.ID,
		network:            net,
		userMeta:           make(map[string]*UserMeta),
		monitorStatus:      make(map[string]bool),
		monitorArmed:       make(map[string]bool),
		enabledCaps:        make(map[string]bool),
		serverCapabilities: &ServerCapabilities{},
		knownBots:          make(map[string]bool),
		whoisInProgress:    make(map[string]*WhoisInfo),
	}
	return c, counter
}

func TestApplyWhoxRow_BotFlagMarksBot(t *testing.T) {
	c, counter := newWhoxBotTestClient(t)
	c.serverCapabilities.BotModeChar = 'B'

	// Params: [ourNick, token, channel, user, host, nick, flags, account, realname]
	// flags "H*B" = here, oper, bot.
	c.applyWhoxRow([]string{"me", "332", "#chan", "~u", "host", "robodan", "H*B", "0", "Robo Dan"})

	_, got := waitForBotCount(t, counter, 1)
	found := false
	for _, n := range got {
		if n == "robodan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected robodan to be marked as a bot from WHOX B flag; got nicks %v", got)
	}
}

func TestApplyWhoxRow_NoBotFlag(t *testing.T) {
	c, _ := newWhoxBotTestClient(t)
	c.serverCapabilities.BotModeChar = 'B'

	// Params: [ourNick, token, channel, user, host, nick, flags, account, realname]
	c.applyWhoxRow([]string{"me", "332", "#chan", "~u", "host", "alice", "H", "0", "Alice"})

	if botNickSet(c)["alice"] {
		t.Fatalf("alice has no B flag and must not be marked a bot")
	}
}
