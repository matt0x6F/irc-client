package irc

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// capturingSub is a minimal events.Subscriber that forwards received events to
// a channel so the test can inspect them.
type capturingSub struct{ got chan events.Event }

func (s capturingSub) OnEvent(ev events.Event) { s.got <- ev }

func newPrivmsgTestClient(t *testing.T) *IRCClient {
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
	c := &IRCClient{
		eventBus:    events.NewEventBus(),
		storage:     s,
		networkID:   net.ID,
		network:     net,
		userMeta:    make(map[string]*UserMeta),
		enabledCaps: map[string]bool{},
	}
	return c
}

func TestActionEmitsMessageReceived(t *testing.T) {
	c := newPrivmsgTestClient(t)
	got := make(chan events.Event, 1)
	c.eventBus.Subscribe(EventMessageReceived, capturingSub{got: got})

	// CTCP ACTION: body is \x01ACTION waves\x01
	line := ":alittlefang!u@h PRIVMSG #chan :\x01ACTION waves at matt0x6f\x01"
	msg, err := ircmsg.ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handlePrivmsg(msg)

	select {
	case ev := <-got:
		if ev.Data["isAction"] != true {
			t.Fatalf("isAction = %v; want true", ev.Data["isAction"])
		}
		if ev.Data["message"] != "waves at matt0x6f" {
			t.Fatalf("message = %q; want %q", ev.Data["message"], "waves at matt0x6f")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("action did not emit message.received")
	}
}

func TestNoticeEmitsContextFields(t *testing.T) {
	c := newPrivmsgTestClient(t)
	// Prime the roster so accountFor("chanserv") returns "cs_acct".
	c.applyUserMeta("chanserv", func(m *UserMeta) { m.Account = "cs_acct" })

	got := make(chan events.Event, 4)
	c.eventBus.Subscribe(EventMessageReceived, capturingSub{got: got})

	// PM-path NOTICE (chanserv!s@h prefix + target == our nick) so it emits.
	line := "@msgid=n1 :chanserv!s@h NOTICE matt0x6f :hello there"
	msg, err := ircmsg.ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handleNotice(msg)

	select {
	case ev := <-got:
		if ev.Data["account"] != "cs_acct" {
			t.Fatalf("account = %v; want cs_acct", ev.Data["account"])
		}
		if ev.Data["networkName"] != "Libera" {
			t.Fatalf("networkName = %v; want Libera", ev.Data["networkName"])
		}
		if ev.Data["msgid"] != "n1" {
			t.Fatalf("msgid = %v; want n1", ev.Data["msgid"])
		}
		if _, ok := ev.Data["messageUnix"].(int64); !ok {
			t.Fatalf("messageUnix missing or not int64: %T", ev.Data["messageUnix"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message.received event emitted for notice")
	}
}

func TestPrivmsgEmitsContextFields(t *testing.T) {
	c := newPrivmsgTestClient(t)
	// Roster knows alittlefang's account.
	c.applyUserMeta("alittlefang", func(m *UserMeta) { m.Account = "fang_acct" })

	got := make(chan events.Event, 4)
	c.eventBus.Subscribe(EventMessageReceived, capturingSub{got: got})

	line := "@msgid=xyz789 :alittlefang!u@h PRIVMSG #chan :matt0x6f hello"
	msg, err := ircmsg.ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handlePrivmsg(msg) // the function the PRIVMSG callback delegates to

	select {
	case ev := <-got:
		if ev.Data["account"] != "fang_acct" {
			t.Fatalf("account = %v; want fang_acct", ev.Data["account"])
		}
		if ev.Data["msgid"] != "xyz789" {
			t.Fatalf("msgid = %v; want xyz789", ev.Data["msgid"])
		}
		if ev.Data["networkName"] != "Libera" {
			t.Fatalf("networkName = %v; want Libera", ev.Data["networkName"])
		}
		if ev.Data["isAction"] != false {
			t.Fatalf("isAction = %v; want false", ev.Data["isAction"])
		}
		if _, ok := ev.Data["messageUnix"].(int64); !ok {
			t.Fatalf("messageUnix missing or not int64: %T", ev.Data["messageUnix"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message.received event emitted")
	}
}

// TestInboundReplyTagPersisted dispatches a PRIVMSG carrying +draft/reply and
// asserts the stored row carries the parent msgid in ReplyMsgID.
func TestInboundReplyTagPersisted(t *testing.T) {
	c := newPrivmsgTestClient(t)

	line := "@msgid=child-1;+draft/reply=parent-1 :alittlefang!u@h PRIVMSG #chan :replying to parent"
	msg, err := ircmsg.ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handlePrivmsg(msg)

	got, err := c.storage.GetMessageByMsgID(c.networkID, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReplyMsgID != "parent-1" {
		t.Fatalf("reply tag not persisted: %q", got.ReplyMsgID)
	}
}

// TestInboundChannelContextPersisted dispatches a PM PRIVMSG with
// +draft/channel-context and asserts the stored row carries the channel name.
func TestInboundChannelContextPersisted(t *testing.T) {
	c := newPrivmsgTestClient(t)

	// PM to our nick carrying a channel-context tag.
	line := "@msgid=pm-1;+draft/channel-context=#dev :alittlefang!u@h PRIVMSG matt0x6f :this is about #dev"
	msg, err := ircmsg.ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handlePrivmsg(msg)

	got, err := c.storage.GetMessageByMsgID(c.networkID, "pm-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ChannelContext != "#dev" {
		t.Fatalf("channel-context tag not persisted: %q", got.ChannelContext)
	}
}

// TestInboundReplyTagEmitted asserts the event Data map includes replyMsgid and
// channelContext keys after a tagged PRIVMSG.
func TestInboundReplyTagEmitted(t *testing.T) {
	c := newPrivmsgTestClient(t)

	got := make(chan events.Event, 1)
	c.eventBus.Subscribe(EventMessageReceived, capturingSub{got: got})

	line := "@msgid=child-2;+draft/reply=parent-2 :alittlefang!u@h PRIVMSG #chan :another reply"
	msg, err := ircmsg.ParseLine(line)
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handlePrivmsg(msg)

	select {
	case ev := <-got:
		if ev.Data["replyMsgid"] != "parent-2" {
			t.Fatalf("replyMsgid in event = %v; want parent-2", ev.Data["replyMsgid"])
		}
		if ev.Data["channelContext"] != "" {
			t.Fatalf("channelContext in event = %v; want empty", ev.Data["channelContext"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message.received event emitted")
	}
}
