package irc

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// blockingSubscriber stalls the event dispatcher until released, standing in for
// a slow plugin subscriber that can't keep up with a roster flood.
type blockingSubscriber struct {
	release chan struct{}
	once    sync.Once
}

func newBlockingSubscriber() *blockingSubscriber {
	return &blockingSubscriber{release: make(chan struct{})}
}

func (b *blockingSubscriber) OnEvent(events.Event) { <-b.release }

func (b *blockingSubscriber) Release() { b.once.Do(func() { close(b.release) }) }

// TestApplyUserMetaDoesNotBlockOnSaturatedBus reproduces the reconnect-churn
// root cause. Processing a large NAMES/WHO roster reply (e.g. ##chat with ~800
// users) emits one user-meta event per user, and those emits run on the irc-go
// read goroutine. EventBus.Emit applies backpressure onto the emitter when its
// queue fills, so a plugin subscriber that can't drain fast enough blocks the
// read loop — the socket stops draining, the server's SendQ overflows, and the
// link is dropped ~seconds after joining. The read-loop path must therefore
// never block on a saturated bus.
func TestApplyUserMetaDoesNotBlockOnSaturatedBus(t *testing.T) {
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
	t.Cleanup(bus.Close)
	blocker := newBlockingSubscriber()
	t.Cleanup(blocker.Release)
	bus.Subscribe(EventUserMetaChanged, blocker)

	c := &IRCClient{
		eventBus:           bus,
		storage:            s,
		networkID:          net.ID,
		network:            net,
		userMeta:           make(map[string]*UserMeta),
		enabledCaps:        make(map[string]bool),
		serverCapabilities: &ServerCapabilities{},
	}

	// Simulate the read loop seeding a large channel roster: far more distinct
	// nicks than the event-queue depth. With the blocker stalling the
	// dispatcher, the queue fills and — under the bug — Emit blocks the read
	// loop inside this goroutine.
	const nicks = 3000
	done := make(chan struct{})
	go func() {
		for i := 0; i < nicks; i++ {
			nick := fmt.Sprintf("user%d", i)
			c.applyUserMeta(nick, func(m *UserMeta) { m.Away = true })
		}
		close(done)
	}()

	select {
	case <-done:
		// Read loop stayed responsive under a saturated bus.
	case <-time.After(3 * time.Second):
		t.Fatal("applyUserMeta blocked the read loop on a saturated event bus — the socket would stall and the server would drop the link")
	}
}
