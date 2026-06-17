package irc

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// botCounter is an events.Subscriber that counts EventBotDetected emissions and
// records the nicks carried in their payloads. Emit dispatches asynchronously
// (one goroutine per subscriber), so tests poll snapshot() rather than read fields.
type botCounter struct {
	mu    sync.Mutex
	count int
	nicks []string
}

func (b *botCounter) OnEvent(e events.Event) {
	b.mu.Lock()
	b.count++
	if nick, ok := e.Data["nickname"].(string); ok {
		b.nicks = append(b.nicks, nick)
	}
	b.mu.Unlock()
}

func (b *botCounter) snapshot() (int, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count, append([]string(nil), b.nicks...)
}

// waitForBotCount polls until at least n emissions have been observed (Emit is
// async), failing the test if they don't arrive within the deadline.
func waitForBotCount(t *testing.T, counter *botCounter, n int) (int, []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if count, nicks := counter.snapshot(); count >= n {
			return count, nicks
		}
		time.Sleep(5 * time.Millisecond)
	}
	count, nicks := counter.snapshot()
	t.Fatalf("timed out waiting for %d bot events; got %d (%v)", n, count, nicks)
	return count, nicks
}

// newBotTestClient builds a minimal IRCClient with the fields markBot and the
// WHOIS-bot path touch: an event bus, storage, a network, and the knownBots map.
func newBotTestClient(t *testing.T) (*IRCClient, *botCounter) {
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
		eventBus:        bus,
		storage:         s,
		networkID:       net.ID,
		network:         net,
		knownBots:       make(map[string]bool),
		whoisInProgress: make(map[string]*WhoisInfo),
	}
	return c, counter
}

// botNickSet returns BotNicks as a set for order-independent assertions.
func botNickSet(c *IRCClient) map[string]bool {
	set := make(map[string]bool)
	for _, n := range c.BotNicks() {
		set[n] = true
	}
	return set
}

func TestMarkBotIdempotentAndLowercased(t *testing.T) {
	c, counter := newBotTestClient(t)

	// Two calls for the same nick (different case) should record one lowercased
	// entry and emit EventBotDetected exactly once.
	c.markBot("BotServ")
	c.markBot("botserv") // duplicate, must not re-emit

	got := c.BotNicks()
	if len(got) != 1 || got[0] != "botserv" {
		t.Fatalf("BotNicks() = %v, want [botserv]", got)
	}

	// Emit is async: wait for the first emission to land, then confirm a stray
	// second one never arrives.
	waitForBotCount(t, counter, 1)
	time.Sleep(50 * time.Millisecond)
	count, nicks := counter.snapshot()
	if count != 1 {
		t.Fatalf("EventBotDetected emitted %d times, want 1", count)
	}
	if len(nicks) != 1 || nicks[0] != "botserv" {
		t.Fatalf("event nicks = %v, want [botserv]", nicks)
	}
}

func TestMarkBotEmptyNickIgnored(t *testing.T) {
	c, _ := newBotTestClient(t)
	c.markBot("")
	if got := c.BotNicks(); len(got) != 0 {
		t.Fatalf("BotNicks() = %v, want empty", got)
	}
}

func TestBotTagMarksSender(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "tagged PRIVMSG marks sender",
			raw:  "@bot :helper!h@host PRIVMSG #chan :hello humans",
			want: true,
		},
		{
			name: "valued bot tag still marks (value ignored per spec)",
			raw:  "@bot=1 :helper!h@host PRIVMSG #chan :hello",
			want: true,
		},
		{
			name: "untagged PRIVMSG does not mark",
			raw:  ":human!h@host PRIVMSG #chan :hi there",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newBotTestClient(t)
			e, err := ircmsg.ParseLine(tc.raw)
			if err != nil {
				t.Fatalf("ParseLine(%q): %v", tc.raw, err)
			}
			c.maybeMarkBotFromTag(e)

			marked := botNickSet(c)["helper"]
			if marked != tc.want {
				t.Fatalf("helper marked = %v, want %v (BotNicks=%v)", marked, tc.want, c.BotNicks())
			}
		})
	}
}

func TestHandleNoticeBotTagMarksSender(t *testing.T) {
	// End-to-end through the real NOTICE handler: a tagged channel notice should
	// mark the sender as a bot.
	c, _ := newBotTestClient(t)
	c.namesInProgress = make(map[string]bool)
	if err := c.storage.CreateChannel(&storage.Channel{NetworkID: c.networkID, Name: "#chan", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	e, err := ircmsg.ParseLine("@bot :buildbot!b@h NOTICE #chan :build passed")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handleNotice(e)

	if !botNickSet(c)["buildbot"] {
		t.Fatalf("buildbot not marked as bot via NOTICE; BotNicks=%v", c.BotNicks())
	}
}

func TestHandleWhoisBotSetsIsBotAndMarks(t *testing.T) {
	c, _ := newBotTestClient(t)

	// :server 335 <me> <nick> :is a bot
	e, err := ircmsg.ParseLine(":calcium.libera.chat 335 matt0x6f helperbot :is a bot")
	if err != nil {
		t.Fatalf("ParseLine: %v", err)
	}
	c.handleWhoisBot(e)

	c.whoisMu.Lock()
	info := c.whoisInProgress["helperbot"]
	c.whoisMu.Unlock()
	if info == nil || !info.IsBot {
		t.Fatalf("whoisInProgress[helperbot].IsBot not set: %+v", info)
	}

	if !botNickSet(c)["helperbot"] {
		t.Fatalf("helperbot not added to bot set; BotNicks=%v", c.BotNicks())
	}
}
