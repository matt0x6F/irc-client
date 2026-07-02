package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/security"
	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/zalando/go-keyring"
)

// Regression tests for the delete-vs-auto-reconnect race that resurrected
// deleted networks: DeleteNetwork tears down the connection, the resulting
// EventConnectionLost trips handleConnectionLost, and — with no
// intentional-disconnect marker — the reconnect path re-created the network
// row via buildNetworkFromConfig's create-if-missing branch. On CI this left
// the deleted network alive and connected, poisoning the shared e2e DB.

func newDeleteTestApp(t *testing.T) *App {
	t.Helper()
	keyring.MockInit() // in-memory keychain
	s, err := storage.NewStorage(filepath.Join(t.TempDir(), "test.db"), 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &App{
		storage:               s,
		eventBus:              events.NewEventBus(),
		creds:                 security.NewCredentialStore(security.NewKeychain()),
		ircClients:            make(map[int64]*irc.IRCClient),
		intentionalDisconnect: make(map[int64]bool),
		reconnectingNetworks:  make(map[int64]bool),
		stsUpgrading:          make(map[int64]bool),
	}
}

func createDeleteTestNetwork(t *testing.T, a *App, name string) *storage.Network {
	t.Helper()
	net := &storage.Network{
		Name: name, Address: "localhost", Port: 1, Nickname: "nick",
		Username: "user", Realname: "real", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := a.storage.CreateNetwork(net); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	return net
}

// Deleting a network that has a live client must mark the teardown as
// intentional so handleConnectionLost doesn't treat the resulting drop as an
// unexpected loss and auto-reconnect the network we just deleted.
func TestDeleteNetworkMarksIntentionalDisconnect(t *testing.T) {
	a := newDeleteTestApp(t)
	net := createDeleteTestNetwork(t, a, "doomed")

	client := irc.NewIRCClient(net, a.eventBus, a.storage)
	a.ircClients[net.ID] = client

	if err := a.DeleteNetwork(net.ID); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}

	a.mu.Lock()
	intentional := a.intentionalDisconnect[net.ID]
	a.mu.Unlock()
	if !intentional {
		t.Error("DeleteNetwork did not mark the disconnect intentional; auto-reconnect can resurrect the network")
	}

	if nets, _ := a.storage.GetNetworks(); len(nets) != 0 {
		t.Errorf("network row survived deletion: %+v", nets)
	}
}

// A reconnect is by definition for a network that already exists. When the row
// is gone (deleted mid-teardown), connectNetwork must abort — not fall into
// buildNetworkFromConfig's create-if-missing branch and resurrect the network.
func TestReconnectDoesNotResurrectDeletedNetwork(t *testing.T) {
	a := newDeleteTestApp(t)

	cfg := NetworkConfig{
		Name: "ghost", Address: "localhost", Port: 1,
		Nickname: "nick", Username: "user", Realname: "real",
	}

	if err := a.connectNetwork(cfg, true); err == nil {
		t.Error("connectNetwork(reconnect=true) succeeded for a deleted network, want error")
	}

	nets, err := a.storage.GetNetworks()
	if err != nil {
		t.Fatalf("GetNetworks: %v", err)
	}
	if len(nets) != 0 {
		t.Errorf("reconnect resurrected a deleted network: %+v", nets)
	}
}
