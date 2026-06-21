package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

// newAutoConnectTestApp builds an App backed by a real temp-dir SQLite store,
// enough to exercise buildNetworkFromConfig's persistence behavior.
func newAutoConnectTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := storage.NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &App{
		storage:            s,
		connectingNetworks: make(map[string]chan struct{}),
	}
}

// TestConnectDoesNotClobberAutoConnect reproduces the bug where connecting to a
// network with a config that omits auto_connect would reset a previously-enabled
// auto_connect preference back to false.
func TestConnectDoesNotClobberAutoConnect(t *testing.T) {
	a := newAutoConnectTestApp(t)

	servers := []ServerConfig{{Address: "irc.libera.chat", Port: 6697, TLS: true, Order: 0}}
	cfg := NetworkConfig{
		Name:     "Libera.chat",
		Nickname: "tester",
		Username: "tester",
		Realname: "Tester",
		Servers:  servers,
	}

	// First save the network (no auto-connect yet).
	net, err := a.buildNetworkFromConfig(cfg, servers, true)
	if err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// User enables auto-connect (e.g. via the context-menu toggle).
	if err := a.storage.UpdateNetworkAutoConnect(net.ID, true); err != nil {
		t.Fatalf("UpdateNetworkAutoConnect: %v", err)
	}

	// User now manually connects. The connect-time config carries no
	// auto_connect value (defaults false). This must NOT reset the stored flag.
	if _, err := a.buildNetworkFromConfig(cfg, servers, false); err != nil {
		t.Fatalf("connect rebuild: %v", err)
	}

	got, err := a.storage.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.AutoConnect {
		t.Fatal("auto_connect was reset to false by a connect operation; expected it to persist")
	}
}

// newConnectionLostTestApp builds an App with the full set of guard maps that
// handleConnectionLost touches, so the auto-reconnect decision can be exercised
// without a live IRC client.
func newConnectionLostTestApp(t *testing.T) *App {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := storage.NewStorage(dbPath, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &App{
		storage:               s,
		connectingNetworks:    make(map[string]chan struct{}),
		reconnectingNetworks:  make(map[int64]bool),
		connectionGapOpen:     make(map[int64]bool),
		stsUpgrading:          make(map[int64]bool),
		intentionalDisconnect: make(map[int64]bool),
	}
}

// TestIntentionalDisconnectSuppressesAutoReconnect reproduces the bug where
// hitting Disconnect on an auto-connect network immediately reconnects: the
// deliberate teardown rode the same EventConnectionLost path as an unexpected
// drop. A network flagged as intentionally disconnected must NOT start a
// reconnect, and the flag must be consumed so a later genuine drop still does.
func TestIntentionalDisconnectSuppressesAutoReconnect(t *testing.T) {
	a := newConnectionLostTestApp(t)

	net := &storage.Network{
		Name:        "Libera.chat",
		Address:     "irc.libera.chat",
		Port:        6697,
		TLS:         true,
		Nickname:    "tester",
		AutoConnect: true,
	}
	if err := a.storage.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// The user pressed Disconnect: DisconnectNetwork flags the deliberate teardown.
	a.mu.Lock()
	a.intentionalDisconnect[net.ID] = true
	a.mu.Unlock()

	a.handleConnectionLost(net.ID)

	a.mu.RLock()
	reconnecting := a.reconnectingNetworks[net.ID]
	stillFlagged := a.intentionalDisconnect[net.ID]
	a.mu.RUnlock()

	if reconnecting {
		t.Fatal("intentional disconnect started an auto-reconnect; expected it to be suppressed")
	}
	if stillFlagged {
		t.Fatal("intentional-disconnect flag was not consumed; a later genuine drop would be wrongly suppressed")
	}
}

// TestSTSUpgradeDisconnectDoesNotLeakIntentionalFlag guards the interaction
// between the two deliberate-teardown paths. An STS upgrade routes through
// DisconnectNetwork (which sets the intentional-disconnect marker) but its
// EventConnectionLost short-circuits at the stsUpgrading guard. The marker must
// still be consumed there, otherwise it leaks onto the next genuine drop and
// wrongly suppresses that auto-reconnect.
func TestSTSUpgradeDisconnectDoesNotLeakIntentionalFlag(t *testing.T) {
	a := newConnectionLostTestApp(t)

	net := &storage.Network{
		Name:        "Libera.chat",
		Address:     "irc.libera.chat",
		Port:        6697,
		TLS:         true,
		Nickname:    "tester",
		AutoConnect: true,
	}
	if err := a.storage.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	// STS upgrade in progress: both guards are armed for the deliberate teardown.
	a.mu.Lock()
	a.stsUpgrading[net.ID] = true
	a.intentionalDisconnect[net.ID] = true
	a.mu.Unlock()

	a.handleConnectionLost(net.ID)

	a.mu.RLock()
	leaked := a.intentionalDisconnect[net.ID]
	a.mu.RUnlock()
	if leaked {
		t.Fatal("intentional-disconnect flag leaked past the STS-upgrade guard; a later genuine drop would be wrongly suppressed")
	}
}

// TestSaveNetworkPersistsAutoConnect confirms the explicit-save path can still
// set the auto_connect preference.
func TestSaveNetworkPersistsAutoConnect(t *testing.T) {
	a := newAutoConnectTestApp(t)

	servers := []ServerConfig{{Address: "irc.libera.chat", Port: 6697, TLS: true, Order: 0}}
	cfg := NetworkConfig{
		Name:        "Libera.chat",
		Nickname:    "tester",
		Username:    "tester",
		Realname:    "Tester",
		Servers:     servers,
		AutoConnect: true,
	}

	net, err := a.buildNetworkFromConfig(cfg, servers, true)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := a.storage.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.AutoConnect {
		t.Fatal("explicit save with AutoConnect=true did not persist")
	}
}
