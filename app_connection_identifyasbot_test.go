package main

import (
	"testing"
)

// TestIdentifyAsBotPersistsOnSave confirms that buildNetworkFromConfig with
// persistAutoConnect=true writes IdentifyAsBot=true to storage, and that the
// storage.Network→NetworkConfig conversion carries the field back.
func TestIdentifyAsBotPersistsOnSave(t *testing.T) {
	a := newAutoConnectTestApp(t)

	servers := []ServerConfig{{Address: "irc.libera.chat", Port: 6697, TLS: true, Order: 0}}
	cfg := NetworkConfig{
		Name:          "Libera.chat",
		Nickname:      "botuser",
		Username:      "botuser",
		Realname:      "Bot User",
		Servers:       servers,
		AutoConnect:   true,
		IdentifyAsBot: true,
	}

	net, err := a.buildNetworkFromConfig(cfg, servers, true)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := a.storage.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.IdentifyAsBot {
		t.Fatal("explicit save with IdentifyAsBot=true did not persist")
	}
}

// TestIdentifyAsBotNotClobberedOnConnect confirms that a connect-time
// buildNetworkFromConfig (persistAutoConnect=false) does NOT reset a stored
// IdentifyAsBot preference back to false.
func TestIdentifyAsBotNotClobberedOnConnect(t *testing.T) {
	a := newAutoConnectTestApp(t)

	servers := []ServerConfig{{Address: "irc.libera.chat", Port: 6697, TLS: true, Order: 0}}
	cfg := NetworkConfig{
		Name:     "Libera.chat",
		Nickname: "botuser",
		Username: "botuser",
		Realname: "Bot User",
		Servers:  servers,
	}

	// First save (no IdentifyAsBot yet).
	net, err := a.buildNetworkFromConfig(cfg, servers, true)
	if err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// Directly set IdentifyAsBot in storage (simulating a prior save with it enabled).
	net.IdentifyAsBot = true
	if err := a.storage.UpdateNetwork(net); err != nil {
		t.Fatalf("UpdateNetwork: %v", err)
	}

	// Connect-time rebuild: IdentifyAsBot is false in cfg (not set), must NOT clobber.
	if _, err := a.buildNetworkFromConfig(cfg, servers, false); err != nil {
		t.Fatalf("connect rebuild: %v", err)
	}

	got, err := a.storage.GetNetwork(net.ID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if !got.IdentifyAsBot {
		t.Fatal("identify_as_bot was reset to false by a connect operation; expected it to persist")
	}
}

// TestBuildReconnectConfigCarriesIdentifyAsBot confirms that
// buildReconnectConfig (storage.Network→NetworkConfig) maps the IdentifyAsBot
// field back to the DTO so reconnect/autoconnect paths see the value.
func TestBuildReconnectConfigCarriesIdentifyAsBot(t *testing.T) {
	a := newAutoConnectTestApp(t)

	servers := []ServerConfig{{Address: "irc.libera.chat", Port: 6697, TLS: true, Order: 0}}
	cfg := NetworkConfig{
		Name:          "Libera.chat",
		Nickname:      "botuser",
		Username:      "botuser",
		Realname:      "Bot User",
		Servers:       servers,
		IdentifyAsBot: true,
	}

	net, err := a.buildNetworkFromConfig(cfg, servers, true)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// buildReconnectConfig is the storage.Network→NetworkConfig conversion used
	// for reconnect and auto-connect paths.
	result, err := a.buildReconnectConfig(net.ID, net)
	if err != nil {
		t.Fatalf("buildReconnectConfig: %v", err)
	}
	if !result.IdentifyAsBot {
		t.Fatal("buildReconnectConfig did not carry IdentifyAsBot=true back to the config DTO")
	}
}
