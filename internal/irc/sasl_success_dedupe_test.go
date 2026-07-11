package irc

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

type saslSuccessCounter struct {
	mu    sync.Mutex
	count int
}

func (s *saslSuccessCounter) OnEvent(events.Event) {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
}

func (s *saslSuccessCounter) snapshot() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// TestSASLSuccessSurfacedOncePerConnection: Libera sends both RPL_LOGGEDIN (900)
// and RPL_SASLSUCCESS (903) for a single successful authentication, so
// observeSASLSuccess is invoked twice. It must surface the "SASL authentication
// successful" status line and EventSASLSuccess only once — a duplicate reads to
// users like a second connection (the churn-investigation false alarm).
func TestSASLSuccessSurfacedOncePerConnection(t *testing.T) {
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
	counter := &saslSuccessCounter{}
	bus.Subscribe(EventSASLSuccess, counter)

	c := &IRCClient{
		eventBus:  bus,
		storage:   s,
		networkID: net.ID,
		network:   net,
	}

	// Both success numerics arrive for one authentication.
	c.observeSASLSuccess()
	c.observeSASLSuccess()

	// Allow async delivery, then assert exactly one surfaced event.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && counter.snapshot() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // let any duplicate surface
	if got := counter.snapshot(); got != 1 {
		t.Fatalf("EventSASLSuccess fired %d times, want 1 (duplicate SASL success reads as a second connection)", got)
	}
}
