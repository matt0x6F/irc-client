package irc

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newNickTestClient builds a real IRCClient backed by temp-file storage and a
// real event bus, with the preferred nick "matt0x6f". conn is intentionally nil:
// the nick handlers under test must operate on client state, storage, and the
// event bus only — never the live connection.
func newNickTestClient(t *testing.T) *IRCClient {
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
	return &IRCClient{
		eventBus:  events.NewEventBus(),
		storage:   s,
		networkID: net.ID,
		network:   net,
	}
}

// eventCollector is a test Subscriber that forwards events to a channel so tests
// can assert on the asynchronously-emitted events.
type eventCollector struct{ ch chan events.Event }

func (e eventCollector) OnEvent(ev events.Event) { e.ch <- ev }

func parseLine(t *testing.T, raw string) ircmsg.Message {
	t.Helper()
	e, err := ircmsg.ParseLine(raw)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", raw, err)
	}
	return e
}

func statusMessages(t *testing.T, c *IRCClient) []storage.Message {
	t.Helper()
	msgs, err := c.storage.GetMessages(c.networkID, nil, 50)
	if err != nil {
		t.Fatalf("GetMessages(status): %v", err)
	}
	return msgs
}

func containsMessage(msgs []storage.Message, substr string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Message, substr) {
			return true
		}
	}
	return false
}

// The collision notice is written exactly once while we're still unregistered.
// The per-keepalive reclaim 433s that arrive after we've registered under an
// alternative nick must be silent — that silence is the fix for the log being
// spammed every 3 minutes.
func TestNickInUseNotifiesOnceThenSilent(t *testing.T) {
	c := newNickTestClient(t)

	collision := parseLine(t, ":server 433 * matt0x6f :Nickname is already in use")
	c.handleNickInUse(collision) // unregistered: one notice
	c.handleNickInUse(collision) // unregistered duplicate: still one

	// Registered under an alternative now; the library re-requests the preferred
	// nick on every keepalive and the server keeps replying 433.
	c.mu.Lock()
	c.currentNick = "matt0x6f_0"
	c.mu.Unlock()
	reclaim := parseLine(t, ":server 433 matt0x6f_0 matt0x6f :Nickname is already in use")
	c.handleNickInUse(reclaim)
	c.handleNickInUse(reclaim)

	msgs := statusMessages(t, c)
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 collision notice, got %d: %+v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0].Message, "matt0x6f") {
		t.Fatalf("notice should name the preferred nick, got %q", msgs[0].Message)
	}
}

// Registering under an alternative nick records the real nick and explains that
// the client will keep trying to reclaim the preferred one.
func TestWelcomeOnAlternativeNickTracksAndExplains(t *testing.T) {
	c := newNickTestClient(t)

	c.handleWelcome(parseLine(t, ":server 001 matt0x6f_0 :Welcome to Libera"))

	if got := c.CurrentNick(); got != "matt0x6f_0" {
		t.Fatalf("CurrentNick = %q, want matt0x6f_0", got)
	}
	if !containsMessage(statusMessages(t, c), "reclaim") {
		t.Fatalf("expected a reclaim-intent status message, got %+v", statusMessages(t, c))
	}
}

// A NICK from our own current nick is recognized as us: the tracked nick
// updates, a nick.changed event fires, and reclaiming the preferred nick is
// announced in the log.
func TestSelfNickReclaimTracksAndAnnounces(t *testing.T) {
	c := newNickTestClient(t)
	c.mu.Lock()
	c.currentNick = "matt0x6f_0"
	c.mu.Unlock()

	col := eventCollector{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventNickChanged, col)

	c.handleNickMessage(parseLine(t, ":matt0x6f_0!u@h NICK matt0x6f"))

	if got := c.CurrentNick(); got != "matt0x6f" {
		t.Fatalf("CurrentNick = %q, want matt0x6f", got)
	}
	select {
	case ev := <-col.ch:
		if ev.Data["nick"] != "matt0x6f" {
			t.Fatalf("event nick = %v, want matt0x6f", ev.Data["nick"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a nick.changed event, got none")
	}
	if !containsMessage(statusMessages(t, c), "Reclaimed") {
		t.Fatalf("expected a 'Reclaimed' status message, got %+v", statusMessages(t, c))
	}
}

// A NICK from a different user must not be mistaken for our own.
func TestThirdPartyNickIgnoredForSelf(t *testing.T) {
	c := newNickTestClient(t)
	c.mu.Lock()
	c.currentNick = "matt0x6f"
	c.mu.Unlock()

	col := eventCollector{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventNickChanged, col)

	c.handleNickMessage(parseLine(t, ":someoneelse!u@h NICK newguy"))

	if got := c.CurrentNick(); got != "matt0x6f" {
		t.Fatalf("CurrentNick changed to %q on a third-party NICK", got)
	}
	select {
	case ev := <-col.ch:
		t.Fatalf("unexpected nick.changed event for third-party NICK: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// good: no self event emitted
	}
}
