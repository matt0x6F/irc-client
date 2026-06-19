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

// A 433 answering a nick the user explicitly requested via /nick must be
// surfaced even though we're already registered (the "funky" post-reconnect
// state where we're stuck on an alternative). The blanket "registered → silent"
// rule that hushes the library's background keepalive reclaims would otherwise
// swallow the user's manual attempt, making it look like nothing happened.
func TestManualNickChangeFailureIsSurfaced(t *testing.T) {
	c := newNickTestClient(t)

	// Registered as an alternative; the user has just run /nick matt0x6f.
	c.mu.Lock()
	c.currentNick = "matt0x6f_2"
	c.pendingManualNick = "matt0x6f"
	c.mu.Unlock()

	c.handleNickInUse(parseLine(t, ":server 433 matt0x6f_2 matt0x6f :Nickname is already in use"))

	if !containsMessage(statusMessages(t, c), "matt0x6f") {
		t.Fatalf("expected a status message about the failed manual nick change, got %+v", statusMessages(t, c))
	}

	// The manual attempt has now been answered once. A repeat 433 (e.g. the next
	// keepalive reclaim of the same nick) must not re-notify and spam the log.
	before := len(statusMessages(t, c))
	c.handleNickInUse(parseLine(t, ":server 433 matt0x6f_2 matt0x6f :Nickname is already in use"))
	if got := len(statusMessages(t, c)); got != before {
		t.Fatalf("expected no further notice after the manual attempt was answered, got %d new", got-before)
	}
}

// A user-initiated /nick can fail for reasons other than 433: an invalid
// nickname (432), a nick held under nick-delay right after a reconnect (437),
// a collision (436), or an empty nick (431). Each must be surfaced once and
// must clear the pending marker.
func TestManualNickErrorsAreSurfaced(t *testing.T) {
	cases := []struct {
		name    string
		pending string
		line    string
		invoke  func(*IRCClient, ircmsg.Message)
		want    string
	}{
		{"erroneous 432", "1badnick", ":server 432 matt0x6f_2 1badnick :Erroneous Nickname", (*IRCClient).handleErroneousNick, "1badnick"},
		{"unavailable 437", "matt0x6f", ":server 437 matt0x6f_2 matt0x6f :Nick/channel is temporarily unavailable", (*IRCClient).handleNickUnavailable, "temporarily unavailable"},
		{"collision 436", "matt0x6f", ":server 436 matt0x6f_2 matt0x6f :Nickname collision KILL", (*IRCClient).handleNickCollision, "collided"},
		{"no nick given 431", "matt0x6f", ":server 431 matt0x6f_2 :No nickname given", (*IRCClient).handleNoNickGiven, "no nickname given"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newNickTestClient(t)
			c.mu.Lock()
			c.currentNick = "matt0x6f_2"
			c.pendingManualNick = tc.pending
			c.mu.Unlock()

			tc.invoke(c, parseLine(t, tc.line))

			if !containsMessage(statusMessages(t, c), tc.want) {
				t.Fatalf("expected a status message containing %q, got %+v", tc.want, statusMessages(t, c))
			}
			c.mu.RLock()
			pending := c.pendingManualNick
			c.mu.RUnlock()
			if pending != "" {
				t.Fatalf("pendingManualNick = %q, want cleared after surfacing the error", pending)
			}
		})
	}
}

// Nick-error numerics that aren't answering a user-initiated /nick stay silent
// (e.g. a 432/437 the server sends unsolicited), just like background 433s.
func TestNickErrorsSilentWithoutPendingManualNick(t *testing.T) {
	c := newNickTestClient(t)
	c.mu.Lock()
	c.currentNick = "matt0x6f"
	c.pendingManualNick = ""
	c.mu.Unlock()

	c.handleErroneousNick(parseLine(t, ":server 432 matt0x6f weird :Erroneous Nickname"))
	c.handleNickUnavailable(parseLine(t, ":server 437 matt0x6f weird :temporarily unavailable"))

	if msgs := statusMessages(t, c); len(msgs) != 0 {
		t.Fatalf("expected no status messages for unsolicited nick errors, got %+v", msgs)
	}
}

// ChangeNick must not leave a pending manual marker behind when the send can't
// happen (disconnected). A stale marker would otherwise make the next
// connection's first background reclaim 433 look like a failed user request.
func TestChangeNickRollsBackPendingOnSendFailure(t *testing.T) {
	c := newNickTestClient(t)
	// connected is false and conn is nil, so the send cannot succeed.

	if err := c.ChangeNick("matt0x6f"); err == nil {
		t.Fatal("expected ChangeNick to fail while disconnected")
	}

	c.mu.RLock()
	pending := c.pendingManualNick
	c.mu.RUnlock()
	if pending != "" {
		t.Fatalf("pendingManualNick = %q, want cleared after a failed send", pending)
	}
}

// A successful self nick change clears any pending manual request, so a later
// background reclaim 433 for that same nick doesn't get mistaken for the user's
// (already-fulfilled) attempt.
func TestSelfNickChangeClearsPendingManualNick(t *testing.T) {
	c := newNickTestClient(t)
	c.mu.Lock()
	c.currentNick = "matt0x6f_2"
	c.pendingManualNick = "matt0x6f"
	c.mu.Unlock()

	c.handleNickMessage(parseLine(t, ":matt0x6f_2!u@h NICK matt0x6f"))

	c.mu.RLock()
	pending := c.pendingManualNick
	c.mu.RUnlock()
	if pending != "" {
		t.Fatalf("pendingManualNick = %q, want cleared after a successful self nick change", pending)
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
