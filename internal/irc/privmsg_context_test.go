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
