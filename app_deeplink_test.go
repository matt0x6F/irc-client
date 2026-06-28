package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newDeepLinkTestApp builds a minimal App backed by a real temp-file Storage,
// mirroring newMarkerTestApp in app_connection_test.go. Shared by the deep-link
// dispatcher tests (Tasks 3 and 4).
func newDeepLinkTestApp(t *testing.T) (*App, *storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "deeplink-test.db")
	s, err := storage.NewStorage(dbPath, 100, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a := &App{
		storage:              s,
		connectionGapOpen:    make(map[int64]bool),
		reconnectingNetworks: make(map[int64]bool),
		ircClients:           make(map[int64]*irc.IRCClient),
	}
	return a, s
}

// makeNetwork creates a saved network with the given name+address and returns it.
func makeNetwork(t *testing.T, s *storage.Storage, name, address string) *storage.Network {
	t.Helper()
	now := time.Now()
	net := &storage.Network{
		Name: name, Address: address, Port: 6697, Nickname: "me",
		Username: "me", Realname: "Me", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return net
}

func TestFindNetworksByAddress_MultipleMatches(t *testing.T) {
	a, s := newDeepLinkTestApp(t)
	makeNetwork(t, s, "Libera work", "irc.libera.chat")
	makeNetwork(t, s, "Libera personal", "irc.libera.chat")
	makeNetwork(t, s, "OFTC", "irc.oftc.net")

	ids := a.findNetworksByAddress("irc.libera.chat")
	if len(ids) != 2 {
		t.Fatalf("want 2 matches, got %d (%v)", len(ids), ids)
	}
}
