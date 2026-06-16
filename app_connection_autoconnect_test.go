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
