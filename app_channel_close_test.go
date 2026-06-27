package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

func newChannelCloseTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := storage.NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &App{
		storage:  s,
		eventBus: events.NewEventBus(),
	}
}

// CloseChannel must hide the buffer (is_open=false) without parting, even when
// the network has no live IRC client. This is the "stay joined, hide pane" path.
func TestCloseChannelHidesBufferWithoutClient(t *testing.T) {
	a := newChannelCloseTestApp(t)
	now := time.Now()

	// CreateNetwork populates net.ID in place (see app_connection_test.go).
	net := &storage.Network{
		Name:      "Test",
		Address:   "irc.example.org",
		Port:      6697,
		Nickname:  "tester",
		Username:  "tester",
		Realname:  "Tester",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := a.storage.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	ch := &storage.Channel{NetworkID: net.ID, Name: "#cascade", IsOpen: true, CreatedAt: now}
	if err := a.storage.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := a.CloseChannel(net.ID, "#cascade"); err != nil {
		t.Fatalf("CloseChannel: %v", err)
	}

	got, err := a.storage.GetChannelByName(net.ID, "#cascade")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	if got.IsOpen {
		t.Fatal("expected is_open=false after CloseChannel; channel buffer still open")
	}
}
