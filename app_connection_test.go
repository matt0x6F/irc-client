package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// newMarkerTestApp builds a minimal App backed by a real temp-file Storage, wired
// with the maps the connection-marker paths touch. A short flush interval keeps
// buffered writes (WriteMessage) readable within the test's polling window.
func newMarkerTestApp(t *testing.T) (*App, *storage.Storage) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "marker-test.db")
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

func makeMarkerNetwork(t *testing.T, s *storage.Storage) *storage.Network {
	t.Helper()
	now := time.Now()
	net := &storage.Network{
		Name:      "MarkerNet",
		Address:   "irc.example.com",
		Port:      6697,
		Nickname:  "me",
		Username:  "me",
		Realname:  "Me",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return net
}

func makeOpenChannel(t *testing.T, s *storage.Storage, networkID int64, name string) *storage.Channel {
	t.Helper()
	ch := &storage.Channel{NetworkID: networkID, Name: name, IsOpen: true, CreatedAt: time.Now()}
	if err := s.CreateChannel(ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	return ch
}

func countMarkers(msgs []storage.Message, label string) int {
	n := 0
	for _, m := range msgs {
		if m.MessageType == "marker" && m.Message == label {
			n++
		}
	}
	return n
}

// waitForChannelMarkers polls a channel buffer until it holds `want` markers with the
// given label, or fails. Buffered writes flush on a timer, so a poll beats a fixed sleep.
func waitForChannelMarkers(t *testing.T, s *storage.Storage, networkID int64, channelID int64, label string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msgs, err := s.GetMessages(networkID, &channelID, 50)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if countMarkers(msgs, label) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %q marker(s) in channel %d", want, label, channelID)
}

// TestWriteConnectionMarkerFansOut verifies a marker lands in every open channel and
// PM buffer but never in the server/status buffer, and that the widened PM read query
// actually returns the marker row.
func TestWriteConnectionMarkerFansOut(t *testing.T) {
	a, s := newMarkerTestApp(t)
	net := makeMarkerNetwork(t, s)
	ch := makeOpenChannel(t, s, net.ID, "#chan")
	if _, err := s.GetOrCreatePMConversation(net.ID, "alice", net.Nickname); err != nil {
		t.Fatalf("GetOrCreatePMConversation: %v", err)
	}

	a.writeConnectionMarker(net.ID, net, "Disconnected")
	waitForChannelMarkers(t, s, net.ID, ch.ID, "Disconnected", 1)

	// Channel buffer: exactly one marker.
	chMsgs, _ := s.GetMessages(net.ID, &ch.ID, 50)
	if got := countMarkers(chMsgs, "Disconnected"); got != 1 {
		t.Fatalf("channel buffer: want 1 Disconnected marker, got %d", got)
	}

	// PM buffer: marker returned despite the type filter (regression for the widened query).
	pmMsgs, err := s.GetPrivateMessages(net.ID, "alice", net.Nickname, 50)
	if err != nil {
		t.Fatalf("GetPrivateMessages: %v", err)
	}
	if got := countMarkers(pmMsgs, "Disconnected"); got != 1 {
		t.Fatalf("PM buffer: want 1 Disconnected marker, got %d", got)
	}

	// Server/status buffer (channel_id NULL, pm_target NULL): none.
	statusMsgs, _ := s.GetMessages(net.ID, nil, 50)
	if got := countMarkers(statusMsgs, "Disconnected"); got != 0 {
		t.Fatalf("status buffer: want 0 markers, got %d", got)
	}
}

// TestConnectionGapBracketsAndIsIdempotent verifies the full bracket: open writes one
// Disconnected marker (idempotent across repeat events), close writes one Reconnected
// marker and clears the gap (and a redundant close is a no-op).
func TestConnectionGapBracketsAndIsIdempotent(t *testing.T) {
	a, s := newMarkerTestApp(t)
	net := makeMarkerNetwork(t, s)
	ch := makeOpenChannel(t, s, net.ID, "#chan")

	// Open the gap twice — only one Disconnected marker, gap flag set.
	a.openConnectionGap(net.ID, net)
	a.openConnectionGap(net.ID, net)
	waitForChannelMarkers(t, s, net.ID, ch.ID, "Disconnected", 1)

	a.mu.RLock()
	gapOpen := a.connectionGapOpen[net.ID]
	a.mu.RUnlock()
	if !gapOpen {
		t.Fatal("expected connectionGapOpen to be set after openConnectionGap")
	}

	// Close the gap twice — only one Reconnected marker, gap flag cleared.
	a.closeConnectionGap(net.ID, net)
	a.closeConnectionGap(net.ID, net)
	waitForChannelMarkers(t, s, net.ID, ch.ID, "Reconnected", 1)

	chMsgs, _ := s.GetMessages(net.ID, &ch.ID, 50)
	if got := countMarkers(chMsgs, "Disconnected"); got != 1 {
		t.Fatalf("want 1 Disconnected marker, got %d", got)
	}
	if got := countMarkers(chMsgs, "Reconnected"); got != 1 {
		t.Fatalf("want 1 Reconnected marker, got %d", got)
	}

	a.mu.RLock()
	_, stillTracked := a.connectionGapOpen[net.ID]
	a.mu.RUnlock()
	if stillTracked {
		t.Fatal("expected connectionGapOpen entry cleared after closeConnectionGap")
	}
}

// TestCloseConnectionGapNoOpWithoutGap verifies a connect with no open gap (a true
// first-time connect) writes no marker.
func TestCloseConnectionGapNoOpWithoutGap(t *testing.T) {
	a, s := newMarkerTestApp(t)
	net := makeMarkerNetwork(t, s)
	ch := makeOpenChannel(t, s, net.ID, "#chan")

	a.closeConnectionGap(net.ID, net)

	// Allow several flush cycles to prove the absence of a marker.
	time.Sleep(150 * time.Millisecond)
	chMsgs, _ := s.GetMessages(net.ID, &ch.ID, 50)
	if got := countMarkers(chMsgs, "Reconnected"); got != 0 {
		t.Fatalf("want 0 markers without an open gap, got %d", got)
	}
}

func TestAuthFailureBlocksReconnect(t *testing.T) {
	// Default / unset => block reconnect.
	if !authFailureBlocksReconnect("") {
		t.Error("unset setting must block reconnect after auth failure")
	}
	if !authFailureBlocksReconnect("false") {
		t.Error("false setting must block reconnect after auth failure")
	}
	// Opted in => allow reconnect.
	if authFailureBlocksReconnect("true") {
		t.Error("true setting must allow reconnect after auth failure")
	}
}

// TestHandleConnectionLostOpensGap exercises the real entry point: a confirmed drop on
// a network with auto-connect off marks the gap once (no reconnect goroutine spawned)
// and stays idempotent across repeated connection.lost events.
func TestHandleConnectionLostOpensGap(t *testing.T) {
	a, s := newMarkerTestApp(t)
	net := makeMarkerNetwork(t, s) // AutoConnect defaults to false
	ch := makeOpenChannel(t, s, net.ID, "#chan")

	a.handleConnectionLost(net.ID)
	waitForChannelMarkers(t, s, net.ID, ch.ID, "Disconnected", 1)

	// A second lost event for the same drop must not add a second marker.
	a.handleConnectionLost(net.ID)
	time.Sleep(150 * time.Millisecond)

	chMsgs, _ := s.GetMessages(net.ID, &ch.ID, 50)
	if got := countMarkers(chMsgs, "Disconnected"); got != 1 {
		t.Fatalf("want 1 Disconnected marker after repeated lost events, got %d", got)
	}
}
