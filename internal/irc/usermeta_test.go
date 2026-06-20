package irc

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// metaCounter is an events.Subscriber that counts EventUserMetaChanged emissions
// and records the latest payload per nick. Emit dispatches asynchronously, so
// tests poll snapshot() rather than read fields directly.
type metaCounter struct {
	mu     sync.Mutex
	count  int
	latest map[string]map[string]interface{}
}

func newMetaCounter() *metaCounter {
	return &metaCounter{latest: make(map[string]map[string]interface{})}
}

func (m *metaCounter) OnEvent(e events.Event) {
	m.mu.Lock()
	m.count++
	if nick, ok := e.Data["nickname"].(string); ok {
		m.latest[nick] = e.Data
	}
	m.mu.Unlock()
}

func (m *metaCounter) snapshotCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.count
}

func waitForMetaCount(t *testing.T, c *metaCounter, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.snapshotCount() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d user-meta events; got %d", n, c.snapshotCount())
}

// newUserMetaTestClient builds a minimal IRCClient with the fields the roster
// helpers touch: an event bus, storage, a network, and the userMeta map.
func newUserMetaTestClient(t *testing.T) (*IRCClient, *metaCounter) {
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
	counter := newMetaCounter()
	bus.Subscribe(EventUserMetaChanged, counter)

	c := &IRCClient{
		eventBus:      bus,
		storage:       s,
		networkID:     net.ID,
		network:       net,
		userMeta:      make(map[string]*UserMeta),
		monitorStatus: make(map[string]bool),
		monitorArmed:  make(map[string]bool),
		enabledCaps:   make(map[string]bool),
	}
	return c, counter
}

func parse(t *testing.T, raw string) ircmsg.Message {
	t.Helper()
	e, err := ircmsg.ParseLine(raw)
	if err != nil {
		t.Fatalf("ParseLine(%q): %v", raw, err)
	}
	return e
}

func TestAwayNotifySetsAndClears(t *testing.T) {
	c, counter := newUserMetaTestClient(t)

	c.handleAway(parse(t, ":alice!a@h AWAY :back in 5"))
	meta, ok := c.UserMetaFor("alice")
	if !ok || !meta.Away || meta.AwayMessage != "back in 5" {
		t.Fatalf("after AWAY: ok=%v meta=%+v", ok, meta)
	}

	c.handleAway(parse(t, ":alice!a@h AWAY"))
	meta, _ = c.UserMetaFor("ALICE") // case-insensitive lookup
	if meta.Away || meta.AwayMessage != "" {
		t.Fatalf("after un-AWAY: meta=%+v", meta)
	}

	waitForMetaCount(t, counter, 2) // one set, one clear
}

func TestAwayNotifyIdempotent(t *testing.T) {
	c, counter := newUserMetaTestClient(t)

	// Same away state twice → exactly one event.
	c.handleAway(parse(t, ":bob!b@h AWAY :lunch"))
	c.handleAway(parse(t, ":bob!b@h AWAY :lunch"))

	waitForMetaCount(t, counter, 1)
	time.Sleep(50 * time.Millisecond)
	if got := counter.snapshotCount(); got != 1 {
		t.Fatalf("EventUserMetaChanged emitted %d times, want 1", got)
	}
}

func TestAccountNotifySetsAndLogsOut(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleAccount(parse(t, ":carol!c@h ACCOUNT carol_acct"))
	if meta, _ := c.UserMetaFor("carol"); meta.Account != "carol_acct" {
		t.Fatalf("after ACCOUNT: %+v", meta)
	}

	c.handleAccount(parse(t, ":carol!c@h ACCOUNT *"))
	if meta, _ := c.UserMetaFor("carol"); meta.Account != "" {
		t.Fatalf("after logout: %+v", meta)
	}
}

func TestChghostSetsHost(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleChghost(parse(t, ":dave!d@old.host CHGHOST newuser new.host"))
	if meta, _ := c.UserMetaFor("dave"); meta.Host != "newuser@new.host" {
		t.Fatalf("after CHGHOST: %+v", meta)
	}
}

func TestAccountTagAppliesAndStarMeansLoggedOut(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.maybeApplyAccountTag(parse(t, "@account=eve :eve!e@h PRIVMSG #chan :hi"))
	if meta, _ := c.UserMetaFor("eve"); meta.Account != "eve" {
		t.Fatalf("after @account tag: %+v", meta)
	}

	c.maybeApplyAccountTag(parse(t, "@account=* :eve!e@h PRIVMSG #chan :bye"))
	if meta, _ := c.UserMetaFor("eve"); meta.Account != "" {
		t.Fatalf("after @account=*: %+v", meta)
	}

	// Untagged message must not create/alter an entry.
	c.maybeApplyAccountTag(parse(t, ":frank!f@h PRIVMSG #chan :hello"))
	if _, ok := c.UserMetaFor("frank"); ok {
		t.Fatalf("untagged message should not create roster entry for frank")
	}
}

func TestExtendedJoinPopulatesAccount(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.enabledCaps["extended-join"] = true

	// :nick!u@h JOIN #chan accountname :Real Name
	c.maybeApplyExtendedJoin(parse(t, ":grace!g@h JOIN #chan grace_acct :Grace Hopper"))
	if meta, _ := c.UserMetaFor("grace"); meta.Account != "grace_acct" {
		t.Fatalf("after extended JOIN: %+v", meta)
	}

	// "*" account means not logged in: nothing meaningful to record for a
	// previously-unknown nick, so the idempotent update creates no entry.
	c.maybeApplyExtendedJoin(parse(t, ":heidi!h@h JOIN #chan * :Heidi"))
	if meta, _ := c.UserMetaFor("heidi"); meta.Account != "" {
		t.Fatalf("after extended JOIN with *: %+v", meta)
	}
}

func TestExtendedJoinIgnoredWithoutCapOrParams(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// Cap not enabled: a plain JOIN with extra params must be ignored.
	c.maybeApplyExtendedJoin(parse(t, ":ivan!i@h JOIN #chan ivan_acct :Ivan"))
	if _, ok := c.UserMetaFor("ivan"); ok {
		t.Fatalf("extended-join applied while cap disabled")
	}

	// Cap enabled but plain JOIN (no account param): must not panic or record.
	c.enabledCaps["extended-join"] = true
	c.maybeApplyExtendedJoin(parse(t, ":judy!j@h JOIN #chan"))
	if _, ok := c.UserMetaFor("judy"); ok {
		t.Fatalf("plain JOIN created a roster entry")
	}
}

func TestRenameUserMetaFollowsNick(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.handleAway(parse(t, ":mallory!m@h AWAY :afk"))

	c.renameUserMeta("mallory", "mallory_")
	if _, ok := c.UserMetaFor("mallory"); ok {
		t.Fatalf("old nick still has roster entry after rename")
	}
	meta, ok := c.UserMetaFor("mallory_")
	if !ok || !meta.Away || meta.AwayMessage != "afk" {
		t.Fatalf("renamed nick lost roster attributes: ok=%v %+v", ok, meta)
	}
}

func TestRemoveUserMetaDropsEntry(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.handleAccount(parse(t, ":nina!n@h ACCOUNT nina_acct"))

	c.removeUserMeta("NINA") // case-insensitive
	if _, ok := c.UserMetaFor("nina"); ok {
		t.Fatalf("roster entry survived removeUserMeta")
	}
}

func TestAllUserMetaSnapshot(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.handleAway(parse(t, ":oscar!o@h AWAY :brb"))
	c.handleAccount(parse(t, ":peggy!p@h ACCOUNT peggy_acct"))

	all := c.AllUserMeta()
	if len(all) != 2 || !all["oscar"].Away || all["peggy"].Account != "peggy_acct" {
		t.Fatalf("AllUserMeta() = %+v", all)
	}
	// Mutating the returned copy must not affect the client's state.
	all["oscar"] = UserMeta{}
	if meta, _ := c.UserMetaFor("oscar"); !meta.Away {
		t.Fatalf("AllUserMeta returned a live reference, not a copy")
	}
}

func TestTriggerAutoJoinRunsActionExactlyOnce(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	var runs atomic.Int32
	var wg sync.WaitGroup
	c.autoJoinOnce = &sync.Once{}
	c.autoJoinAction = func() {
		runs.Add(1)
		wg.Done()
	}
	wg.Add(1)

	// All three registration signals (376, 422, and the 001-armed fallback) plus
	// concurrent races funnel through the one Once.
	var callers sync.WaitGroup
	for i := 0; i < 10; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			c.triggerAutoJoin()
		}()
	}
	callers.Wait()
	wg.Wait() // action ran

	time.Sleep(50 * time.Millisecond) // let any stray second run surface
	if got := runs.Load(); got != 1 {
		t.Fatalf("auto-join action ran %d times, want 1", got)
	}

	// Re-arming the Once (as Connect does each connection) lets it run again.
	wg.Add(1)
	c.mu.Lock()
	c.autoJoinOnce = &sync.Once{}
	c.mu.Unlock()
	c.triggerAutoJoin()
	wg.Wait()
	if got := runs.Load(); got != 2 {
		t.Fatalf("after re-arm, action ran total %d times, want 2", got)
	}
}
