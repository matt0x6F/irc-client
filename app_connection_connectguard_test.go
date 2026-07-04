package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestDuplicateConnectRejectedWhileInFlight locks in the guard that prevents the
// connect-then-disconnect churn: while a connect attempt for a network is in flight
// (the multi-second dial + CAP/SASL handshake), a second attempt for the SAME
// network must be rejected outright rather than racing the first. Two live clients
// for one network fight over the socket/nick and produce repeated drops.
//
// Every connect entry point (ConnectNetwork, reconnectWithBackoff, autoConnect,
// ConnectSavedNetwork, STS upgrade, wake-reconnect) funnels through connectNetwork,
// so guarding it here covers them all. The guard fires before any dialing, so this
// test is deterministic — no socket is opened.
func TestDuplicateConnectRejectedWhileInFlight(t *testing.T) {
	a := newAutoConnectTestApp(t)

	servers := []ServerConfig{{Address: "irc.libera.chat", Port: 6697, TLS: true, Order: 0}}
	cfg := NetworkConfig{
		Name:     "Libera.chat",
		Nickname: "tester",
		Username: "tester",
		Realname: "Tester",
		Servers:  servers,
	}

	// Persist the network so its ID (and thus the guard key) is stable.
	net, err := a.buildNetworkFromConfig(cfg, servers, false)
	if err != nil {
		t.Fatalf("save network: %v", err)
	}

	// Simulate an attempt already in flight for this network.
	a.connectingNetworks[fmt.Sprintf("network:%d", net.ID)] = make(chan struct{})

	// A second connect for the same network must be rejected without dialing.
	err = a.connectNetwork(cfg, false)
	if err == nil {
		t.Fatal("expected duplicate connect to be rejected while an attempt is in flight, got nil")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("expected 'already in progress' rejection, got: %v", err)
	}
}
