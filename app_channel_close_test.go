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

// Closing a channel you are still joined to must drop it from the sidebar list
// (GetOpenChannels), not just flip is_open. A real JOIN records you in the
// channel_users roster, so this exercises the same state the UI sees.
func TestCloseChannelRemovesJoinedChannelFromSidebar(t *testing.T) {
	a := newChannelCloseTestApp(t)
	now := time.Now()

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
	stored, err := a.storage.GetChannelByName(net.ID, "#cascade")
	if err != nil {
		t.Fatalf("GetChannelByName: %v", err)
	}
	// A self-JOIN adds us to the roster, exactly like client.handleJoin does.
	if err := a.storage.AddChannelUser(stored.ID, net.Nickname, ""); err != nil {
		t.Fatalf("AddChannelUser: %v", err)
	}

	// Sanity: the channel is in the sidebar while open. GetOpenChannels is the
	// bound method the frontend sidebar actually calls.
	open, err := a.GetOpenChannels(net.ID)
	if err != nil {
		t.Fatalf("GetOpenChannels (before): %v", err)
	}
	if !containsChannel(open, "#cascade") {
		t.Fatalf("expected #cascade in sidebar before close, got %v", channelNames(open))
	}

	if err := a.CloseChannel(net.ID, "#cascade"); err != nil {
		t.Fatalf("CloseChannel: %v", err)
	}

	open, err = a.GetOpenChannels(net.ID)
	if err != nil {
		t.Fatalf("GetOpenChannels (after): %v", err)
	}
	if containsChannel(open, "#cascade") {
		t.Fatalf("Close Channel did nothing: #cascade still in sidebar after close, got %v", channelNames(open))
	}
}

func containsChannel(chans []storage.Channel, name string) bool {
	for _, c := range chans {
		if c.Name == name {
			return true
		}
	}
	return false
}

func channelNames(chans []storage.Channel) []string {
	names := make([]string, len(chans))
	for i, c := range chans {
		names[i] = c.Name
	}
	return names
}
