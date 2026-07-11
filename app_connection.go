package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matt0x6f/irc-client/internal/constants"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/imageproc"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/security"
	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/matt0x6f/irc-client/internal/validation"
)

// ServerConfig represents a single server address configuration
type ServerConfig struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	TLS     bool   `json:"tls"`
	Order   int    `json:"order"` // Order for fallback (lower = higher priority)
}

// NetworkConfig represents network configuration
type NetworkConfig struct {
	Name             string         `json:"name"`
	Address          string         `json:"address"` // Deprecated: use Servers
	Port             int            `json:"port"`    // Deprecated: use Servers
	TLS              bool           `json:"tls"`     // Deprecated: use Servers
	Servers          []ServerConfig `json:"servers"` // New: multiple server addresses
	Nickname         string         `json:"nickname"`
	Username         string         `json:"username"`
	Realname         string         `json:"realname"`
	Password         string         `json:"password"`
	SASLEnabled      bool           `json:"sasl_enabled"`
	SASLMechanism    string         `json:"sasl_mechanism"`
	SASLUsername     string         `json:"sasl_username"`
	SASLPassword     string         `json:"sasl_password"`
	SASLExternalCert string         `json:"sasl_external_cert"`
	AutoConnect      bool           `json:"auto_connect"`
	IdentifyAsBot    bool           `json:"identify_as_bot"`
}

// writeNetworkStatus writes a line to a network's status buffer and emits
// status.message on the bus (forwarded to the frontend by OnEvent) so an open
// server-log pane refreshes live. The sync write commits the row before the
// notify, so the reload the event triggers can already see it.
func (a *App) writeNetworkStatus(networkID int64, text string) {
	if err := a.storage.WriteMessageSync(storage.Message{
		NetworkID:   networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     text,
		MessageType: "status",
		Timestamp:   time.Now(),
	}); err != nil {
		logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Failed to write network status line")
	}
	a.eventBus.Emit(events.Event{
		Type:      irc.EventStatusMessage,
		Data:      map[string]interface{}{"networkId": networkID},
		Timestamp: time.Now(),
		Source:    events.EventSourceSystem,
	})
}

// stringPtr converts a string to *string, returning nil for empty strings
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// normalizeServers converts legacy single address to servers array
func (a *App) normalizeServers(config NetworkConfig) []ServerConfig {
	if len(config.Servers) > 0 {
		return config.Servers
	}
	// Backward compatibility: convert single address to servers array
	if config.Address != "" {
		return []ServerConfig{
			{
				Address: config.Address,
				Port:    config.Port,
				TLS:     config.TLS,
				Order:   0,
			},
		}
	}
	return []ServerConfig{}
}

// buildNetworkFromConfig creates or updates a storage.Network from a NetworkConfig
// persistAutoConnect controls whether the network's auto_connect preference is
// written from the config. It must be true only for explicit user edits
// (SaveNetwork); connect operations pass false so they preserve the stored
// value instead of clobbering it with a connect-time config that doesn't carry
// the preference.
func (a *App) buildNetworkFromConfig(config NetworkConfig, servers []ServerConfig, persistAutoConnect bool) (*storage.Network, error) {
	var network *storage.Network

	// Check if network already exists (by name)
	networks, err := a.storage.GetNetworks()
	if err == nil {
		for _, n := range networks {
			if n.Name == config.Name {
				network = &n
				break
			}
		}
	}

	// Resolve the secrets to persist. An empty incoming secret means "unchanged":
	// preserve whatever is already stored so a masked, untouched field in the UI
	// doesn't wipe the saved secret. A new network has nothing to preserve. The
	// secrets themselves are written to the keychain by secureNetworkSecrets
	// below, never into the columns here.
	existing := network != nil
	effPassword := config.Password
	effSASLPassword := config.SASLPassword
	if existing {
		if effPassword == "" {
			effPassword = a.creds.Resolve(network.ID, security.FieldPassword, network.Password)
		}
		if effSASLPassword == "" {
			effSASLPassword = a.creds.Resolve(network.ID, security.FieldSASLPassword, derefStr(network.SASLPassword))
		}
	}

	if network == nil {
		// Create new network record. Secret columns are left blank; secrets go to
		// the keychain in secureNetworkSecrets.
		network = &storage.Network{
			Name:             config.Name,
			Address:          servers[0].Address,
			Port:             servers[0].Port,
			TLS:              servers[0].TLS,
			Nickname:         config.Nickname,
			Username:         config.Username,
			Realname:         config.Realname,
			SASLEnabled:      config.SASLEnabled,
			SASLMechanism:    stringPtr(config.SASLMechanism),
			SASLUsername:     stringPtr(config.SASLUsername),
			SASLExternalCert: stringPtr(config.SASLExternalCert),
			AutoConnect:      persistAutoConnect && config.AutoConnect,
			IdentifyAsBot:    persistAutoConnect && config.IdentifyAsBot,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}

		if err := a.storage.CreateNetwork(network); err != nil {
			return nil, fmt.Errorf("failed to create network: %w", err)
		}
	} else {
		// Update existing network config. Secret columns are blanked here and
		// (re)written to the keychain in secureNetworkSecrets.
		network.Name = config.Name
		network.Nickname = config.Nickname
		network.Username = config.Username
		network.Realname = config.Realname
		network.Password = ""
		network.SASLEnabled = config.SASLEnabled
		network.SASLMechanism = stringPtr(config.SASLMechanism)
		network.SASLUsername = stringPtr(config.SASLUsername)
		network.SASLPassword = nil
		network.SASLExternalCert = stringPtr(config.SASLExternalCert)
		// Only an explicit user edit may change the auto_connect preference;
		// a connect operation must preserve the stored value.
		if persistAutoConnect {
			network.AutoConnect = config.AutoConnect
			network.IdentifyAsBot = config.IdentifyAsBot
		}
		network.UpdatedAt = time.Now()
		if err := a.storage.UpdateNetwork(network); err != nil {
			return nil, fmt.Errorf("failed to update network: %w", err)
		}
	}

	// Move the resolved secrets into the keychain (or plaintext-column fallback).
	if err := a.secureNetworkSecrets(network, effPassword, effSASLPassword); err != nil {
		return nil, err
	}

	// Sync servers: delete old ones and create new ones
	if err := a.storage.DeleteAllServers(network.ID); err != nil {
		return nil, fmt.Errorf("failed to delete old servers: %w", err)
	}
	for _, srv := range servers {
		server := &storage.Server{
			NetworkID: network.ID,
			Address:   srv.Address,
			Port:      srv.Port,
			TLS:       srv.TLS,
			Order:     srv.Order,
			CreatedAt: time.Now(),
		}
		if err := a.storage.CreateServer(server); err != nil {
			return nil, fmt.Errorf("failed to create server: %w", err)
		}
	}

	return network, nil
}

// secureNetworkSecrets stores the network's secrets in the keychain and blanks
// the corresponding DB columns. When the keychain is unavailable it falls back
// to persisting the secret in the plaintext column (so the network still works)
// and logs a warning; the getter then reports CredentialStorageInsecure. It
// re-persists the network to save the blanked or fallback columns. Requires
// network.ID to be set.
func (a *App) secureNetworkSecrets(n *storage.Network, password, saslPassword string) error {
	store := func(field, value string, clear func(), keep func(string)) {
		// An empty value here means "preserve", never "delete". It arises either
		// from a masked field the user left untouched, or — critically — from a
		// resolve that failed to read the existing secret back (a locked/denied
		// keychain, or an unsigned dev build whose signature changed). Writing it
		// through would make Store("") delete the entry, so a single transient
		// read failure would permanently wipe the password. Leave the keychain
		// entry (and any fallback column) exactly as they are; intentional
		// removal happens via CredentialStore.Delete on network deletion.
		if value == "" {
			return
		}
		used, err := a.creds.Store(n.ID, field, value)
		if used {
			clear()
			return
		}
		keep(value)
		logger.Log.Warn().Err(err).Int64("network_id", n.ID).Str("field", field).
			Msg("Keychain unavailable; storing IRC secret in plaintext database column (fallback)")
	}

	store(security.FieldPassword, password,
		func() { n.Password = "" },
		func(v string) { n.Password = v })
	store(security.FieldSASLPassword, saslPassword,
		func() { n.SASLPassword = nil },
		func(v string) { n.SASLPassword = stringPtr(v) })

	if err := a.storage.UpdateNetwork(n); err != nil {
		return fmt.Errorf("failed to persist secured network secrets: %w", err)
	}
	return nil
}

// hydrateNetworkSecrets fills in secrets on an in-memory network from the
// keychain (falling back to any legacy plaintext column). The argument is
// expected to be a throwaway copy used to build a connection, so the resolved
// plaintext is never re-persisted.
func (a *App) hydrateNetworkSecrets(n *storage.Network) {
	n.Password = a.creds.Resolve(n.ID, security.FieldPassword, n.Password)
	if v := a.creds.Resolve(n.ID, security.FieldSASLPassword, derefStr(n.SASLPassword)); v != "" {
		n.SASLPassword = stringPtr(v)
	}
	// SASLExternalCert is a path, not a secret; it stays in the column as-is.
}

// migrateNetworkSecrets lazily moves any legacy plaintext secrets on the stored
// network row into the keychain and blanks the columns, persisting the change.
// A no-op when the columns are already empty or the keychain is unavailable.
func (a *App) migrateNetworkSecrets(n *storage.Network) {
	changed := false
	if moved, _ := a.creds.Migrate(n.ID, security.FieldPassword, n.Password); moved && n.Password != "" {
		n.Password = ""
		changed = true
	}
	if sp := derefStr(n.SASLPassword); sp != "" {
		if moved, _ := a.creds.Migrate(n.ID, security.FieldSASLPassword, sp); moved {
			n.SASLPassword = nil
			changed = true
		}
	}
	if changed {
		if err := a.storage.UpdateNetwork(n); err != nil {
			logger.Log.Warn().Err(err).Int64("network_id", n.ID).Msg("Failed to persist lazy secret migration")
		}
	}
}

// SaveNetwork saves network configuration without connecting
func (a *App) SaveNetwork(config NetworkConfig) error {
	servers := a.normalizeServers(config)
	if len(servers) == 0 {
		return fmt.Errorf("no servers provided")
	}

	// SaveNetwork is the explicit-edit path: honor the auto_connect checkbox.
	if _, err := a.buildNetworkFromConfig(config, servers, true); err != nil {
		return err
	}
	// Broadcast so every window (notably the main window, when the save was made
	// from the standalone Settings window) refreshes its network list at once
	// instead of waiting on the periodic poll.
	a.emit("networks:changed")
	return nil
}

// ConnectNetwork connects to an IRC network (fresh connect, e.g. user-initiated
// or startup auto-connect). Reconnect after an unexpected drop goes through
// connectNetwork with reconnect=true so the client rejoins its whole session.
func (a *App) ConnectNetwork(config NetworkConfig) error {
	return a.connectNetwork(config, false)
}

// emitConnecting tells the frontend whether a connect attempt is in flight for a
// network. The UI renders a disabled "Connecting…" while true so the network can't
// be told to connect again during the (multi-second) dial + CAP/SASL handshake —
// a second attempt races the first and causes connect-then-drop churn. Goes to the
// Wails runtime (a.emit), not the internal IRC event bus, so it never blocks.
func (a *App) emitConnecting(networkID int64, connecting bool) {
	a.emit("connection-connecting", map[string]interface{}{
		"networkId":  networkID,
		"connecting": connecting,
	})
}

// connectNetwork connects to an IRC network. When reconnect is true the new
// client is flagged so its auto-join goroutine rejoins every channel we were in
// (not just auto_join channels), restoring nick lists after the drop.
func (a *App) connectNetwork(config NetworkConfig, reconnect bool) error {
	servers := a.normalizeServers(config)

	// Validate network configuration
	serverStructs := make([]struct {
		Address string
		Port    int
	}, len(servers))
	for i, srv := range servers {
		serverStructs[i] = struct {
			Address string
			Port    int
		}{Address: srv.Address, Port: srv.Port}
	}
	if err := validation.ValidateNetworkConfig(config.Name, config.Nickname, config.Username, config.Realname, serverStructs); err != nil {
		return fmt.Errorf("invalid network configuration: %w", err)
	}

	if len(servers) == 0 {
		return fmt.Errorf("no servers provided")
	}

	// A reconnect is by definition for a network that already exists. If the
	// row is gone the network was deleted mid-teardown — abort instead of
	// falling into buildNetworkFromConfig's create-if-missing branch, which
	// would resurrect the deleted network.
	if reconnect {
		found := false
		if networks, err := a.storage.GetNetworks(); err == nil {
			for _, n := range networks {
				if n.Name == config.Name {
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("reconnect aborted: network %q no longer exists", config.Name)
		}
	}

	// Connecting must never mutate the stored auto_connect preference.
	network, err := a.buildNetworkFromConfig(config, servers, false)
	if err != nil {
		return err
	}

	// The UI shows a disabled "Connecting…" while an attempt is in flight so the
	// network can't be told to connect again mid-handshake (a duplicate attempt
	// races the first and produces connect-then-drop churn). Emit connecting=false
	// on EVERY exit path (success, failure, panic) so that state can never get
	// stuck. Guarded by markedConnecting so a call rejected as a duplicate below
	// does not clear the flag owned by the attempt already in progress.
	markedConnecting := false
	defer func() {
		if markedConnecting {
			a.emitConnecting(network.ID, false)
		}
	}()

	// Get servers from database (ordered by priority)
	dbServers, err := a.storage.GetServers(network.ID)
	if err != nil || len(dbServers) == 0 {
		dbServers = make([]storage.Server, len(servers))
		for i, srv := range servers {
			dbServers[i] = storage.Server{
				Address: srv.Address,
				Port:    srv.Port,
				TLS:     srv.TLS,
				Order:   srv.Order,
			}
		}
	}

	networkKey := fmt.Sprintf("network:%d", network.ID)
	logger.Log.Info().Str("network", config.Name).Int("servers", len(dbServers)).Msg("Attempting to connect")

	// Lock for the entire connection process to prevent concurrent connections
	a.mu.Lock()

	// Check if a connection is already in progress for this network
	if _, inProgress := a.connectingNetworks[networkKey]; inProgress {
		logger.Log.Warn().Str("network_key", networkKey).Str("network", config.Name).Int64("id", network.ID).Msg("Connection already in progress, rejecting duplicate connection")
		a.mu.Unlock()
		return fmt.Errorf("connection to %s already in progress", config.Name)
	}

	// Check if already connected to this network (by ID)
	var staleClient *irc.IRCClient
	if existingClient, exists := a.ircClients[network.ID]; exists {
		if existingClient.IsConnectedDirect() {
			logger.Log.Warn().Str("network", config.Name).Int64("id", network.ID).Msg("Already connected, rejecting duplicate connection")
			a.mu.Unlock()
			return fmt.Errorf("already connected to %s", config.Name)
		}
		logger.Log.Debug().Str("network", config.Name).Int64("id", network.ID).Msg("Cleaning up disconnected client before reconnecting")
		staleClient = existingClient
		delete(a.ircClients, network.ID)
	}

	// Mark that we're connecting to this network BEFORE releasing the lock
	connectDone := make(chan struct{})
	a.connectingNetworks[networkKey] = connectDone
	markedConnecting = true
	logger.Log.Debug().Str("network_key", networkKey).Str("network", config.Name).Int64("id", network.ID).Msg("Marked as connecting")

	a.mu.Unlock()

	// Signal the UI that this network is now handshaking (emitted outside a.mu).
	a.emitConnecting(network.ID, true)

	// Deterministically stop the stale client before dialing again. Removing it
	// from the map only drops our reference — its library Loop goroutine keeps
	// running and would auto-reconnect as a ghost session that holds our preferred
	// nick, forcing the new connection onto a fallback nick (and the +r/+f channel
	// forwards that follow). Disconnect sets the quit flag and waits, bounded, for
	// that goroutine to exit. Called outside a.mu so the wait can't stall other
	// networks' operations.
	if staleClient != nil {
		staleClient.Disconnect()
	}

	// Wait a bit for any existing connection to clean up
	time.Sleep(constants.ConnectionCleanupDelay)

	// Double-check after cleanup delay
	a.mu.Lock()
	if existingClient, exists := a.ircClients[network.ID]; exists {
		if existingClient.IsConnectedDirect() {
			logger.Log.Warn().Str("network", config.Name).Int64("id", network.ID).Msg("Connection completed during cleanup delay, cleaning up duplicate attempt")
			delete(a.connectingNetworks, networkKey)
			close(connectDone)
			a.mu.Unlock()
			return fmt.Errorf("already connected to %s (connection completed during setup)", config.Name)
		}
	}
	a.mu.Unlock()

	// Lazily migrate any legacy plaintext secrets on this row into the keychain
	// before dialing, so a reconnect leaves the DB clean.
	a.migrateNetworkSecrets(network)

	// Try connecting to servers in order
	var lastErr error
	for i, srv := range dbServers {
		// Create a temporary network object with this server's address
		tempNetwork := *network
		tempNetwork.Address = srv.Address
		tempNetwork.Port = srv.Port
		tempNetwork.TLS = srv.TLS
		// Resolve secrets from the keychain onto this throwaway copy; the values
		// are used only to build the connection and are never re-persisted.
		a.hydrateNetworkSecrets(&tempNetwork)

		// Enforce STS before dialing: a host with a pending in-session upgrade or a
		// stored policy is forced onto TLS at the policy port. This rewrites only the
		// port/TLS (never adding a plaintext fallback), so a host under STS can never
		// be reached in plaintext and a failed TLS dial won't silently downgrade.
		stsForced := a.applySTS(network.ID, &tempNetwork)

		serverKey := fmt.Sprintf("%s:%d", tempNetwork.Address, tempNetwork.Port)
		logger.Log.Info().Int("current", i+1).Int("total", len(dbServers)).Str("server", serverKey).Bool("sts", stsForced).Msg("Trying server")

		// Write status message
		connectingMsg := fmt.Sprintf("Connecting to %s:%d...", tempNetwork.Address, tempNetwork.Port)
		if stsForced {
			connectingMsg = fmt.Sprintf("Connecting to %s:%d (TLS enforced by STS)...", tempNetwork.Address, tempNetwork.Port)
		}
		a.writeNetworkStatus(network.ID, connectingMsg)

		mechanism := ""
		username := ""
		if tempNetwork.SASLMechanism != nil {
			mechanism = *tempNetwork.SASLMechanism
		}
		if tempNetwork.SASLUsername != nil {
			username = *tempNetwork.SASLUsername
		}
		logger.Log.Debug().
			Str("network", tempNetwork.Name).
			Bool("sasl", tempNetwork.SASLEnabled).
			Str("mechanism", mechanism).
			Str("username", username).
			Msg("Creating IRC client")

		ircClient := irc.NewIRCClient(&tempNetwork, a.eventBus, a.storage)
		ircClient.SetNetworkID(network.ID)
		ircClient.SetReconnecting(reconnect)

		// Try to connect with timeout
		logger.Log.Debug().Str("server", serverKey).Msg("Starting connection attempt")
		connectErr := make(chan error, 1)
		go func() {
			connectErr <- ircClient.Connect()
		}()

		var connErr error
		select {
		case connErr = <-connectErr:
			if connErr != nil {
				logger.Log.Debug().Err(connErr).Str("server", serverKey).Msg("Connection attempt failed")
			} else {
				logger.Log.Debug().Str("server", serverKey).Msg("Connection attempt succeeded")
			}
		case <-time.After(30 * time.Second):
			logger.Log.Warn().Str("server", serverKey).Msg("Connection timeout after 30 seconds")
			ircClient.Disconnect()
			connErr = fmt.Errorf("connection timeout after 30 seconds")
		}

		if connErr != nil {
			lastErr = connErr
			logger.Log.Warn().Err(connErr).Str("server", serverKey).Msg("Failed to connect")

			a.writeNetworkStatus(network.ID, fmt.Sprintf("Failed to connect to %s:%d: %v", srv.Address, srv.Port, connErr))

			continue
		}

		// Success! Store client and clear connection-in-progress flag
		logger.Log.Info().Str("server", serverKey).Int64("network_id", network.ID).Msg("Connection successful")
		a.mu.Lock()

		// Final check: if another connection completed and stored a client, disconnect this one
		if existingClient, exists := a.ircClients[network.ID]; exists && existingClient != ircClient {
			if existingClient.IsConnectedDirect() {
				logger.Log.Warn().Str("network", config.Name).Int64("id", network.ID).Msg("Another connection completed first, disconnecting duplicate")
				delete(a.connectingNetworks, networkKey)
				close(connectDone)
				a.mu.Unlock()
				// Outside a.mu: Disconnect waits (bounded) for the Loop goroutine.
				ircClient.Disconnect()
				return fmt.Errorf("connection to %s already established by another process", config.Name)
			}
		}

		a.ircClients[network.ID] = ircClient
		delete(a.connectingNetworks, networkKey)
		delete(a.reconnectingNetworks, network.ID)
		close(connectDone)
		a.mu.Unlock()

		a.writeNetworkStatus(network.ID, fmt.Sprintf("Connected to %s:%d", srv.Address, srv.Port))

		// A successful reconnect surfaces itself in the channel buffers via the JOIN
		// messages the server echoes as we rejoin, so no separate "Reconnected" marker
		// is written here.

		return nil
	}

	// All servers failed
	logger.Log.Error().Str("network", config.Name).Msg("All connection attempts failed")
	a.mu.Lock()
	delete(a.connectingNetworks, networkKey)
	close(connectDone)
	a.mu.Unlock()

	a.writeNetworkStatus(network.ID, fmt.Sprintf("Failed to connect to any server: %v", lastErr))

	return fmt.Errorf("failed to connect to any server: %w", lastErr)
}

// applySTS rewrites n's port/TLS to satisfy an STS policy for n.Address, returning
// true if it forced an upgrade. It consults, in order: a pending in-session upgrade
// (an insecure advertisement seen this session) and the persisted policy store. IP
// literals are exempt per spec. Because it only ever switches to TLS at the policy
// port — never adding a plaintext fallback — a host under STS cannot be dialed in
// plaintext.
func (a *App) applySTS(networkID int64, n *storage.Network) bool {
	if irc.IsIPLiteral(n.Address) {
		return false
	}

	a.mu.RLock()
	tgt, pending := a.stsUpgrades[networkID]
	a.mu.RUnlock()
	if pending && tgt.host == n.Address && tgt.port > 0 {
		n.Port = tgt.port
		n.TLS = true
		return true
	}

	policy, ok, err := a.storage.GetSTSPolicy(n.Address, time.Now().Unix())
	if err != nil {
		logger.Log.Warn().Err(err).Str("host", n.Address).Msg("Failed to read STS policy")
		return false
	}
	if ok && policy.Port > 0 {
		n.Port = policy.Port
		n.TLS = true
		return true
	}
	return false
}

// upgradeToTLS performs the in-session plaintext→TLS reconnect after a server
// advertised STS over an insecure connection. It guards the network against the
// auto-reconnect path (so the deliberate teardown below isn't treated as a dropped
// connection), tears down the plaintext client, and reconnects — applySTS then forces
// TLS on the pending port. Runs in its own goroutine off the event handler.
func (a *App) upgradeToTLS(networkID int64) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error().Interface("panic", r).Int64("network_id", networkID).Msg("PANIC in STS upgrade")
		}
		a.mu.Lock()
		delete(a.stsUpgrading, networkID)
		a.mu.Unlock()
	}()

	a.mu.Lock()
	a.stsUpgrading[networkID] = true
	a.mu.Unlock()

	network, err := a.storage.GetNetwork(networkID)
	if err != nil || network == nil {
		logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("STS upgrade: network not found")
		return
	}

	config, err := a.buildReconnectConfig(networkID, network)
	if err != nil {
		logger.Log.Error().Err(err).Int64("network_id", networkID).Msg("STS upgrade: failed to build config")
		return
	}

	// Tear down the plaintext connection. The stsUpgrading guard makes the resulting
	// EventConnectionLost a no-op in handleConnectionLost.
	if err := a.DisconnectNetwork(networkID); err != nil {
		logger.Log.Debug().Err(err).Int64("network_id", networkID).Msg("STS upgrade: disconnect (already gone?)")
	}
	time.Sleep(constants.ConnectionCleanupDelay)

	if err := a.connectNetwork(config, true); err != nil {
		logger.Log.Error().Err(err).Int64("network_id", networkID).Msg("STS upgrade: TLS reconnect failed")
		a.writeNetworkStatus(networkID, fmt.Sprintf("STS upgrade to TLS failed: %v", err))
	}
}

// handleConnectionLost handles auto-reconnect when a connection is lost
func (a *App) handleConnectionLost(networkID int64) {
	// Read both deliberate-teardown guards under one lock, consuming the
	// user-disconnect marker unconditionally. Consuming it even when the STS guard
	// short-circuits below matters: an STS upgrade also routes through
	// DisconnectNetwork (which sets the marker), so leaving it set would leak onto
	// the next genuine drop and wrongly suppress that reconnect.
	a.mu.Lock()
	upgrading := a.stsUpgrading[networkID]
	intentional := a.intentionalDisconnect[networkID]
	delete(a.intentionalDisconnect, networkID)
	a.mu.Unlock()

	// A deliberate STS plaintext→TLS upgrade tears down the old connection on
	// purpose; its own goroutine drives the reconnect, so don't race it here.
	if upgrading {
		logger.Log.Debug().Int64("network_id", networkID).Msg("STS upgrade in progress, skipping auto-reconnect")
		return
	}

	// A user-initiated disconnect tears the connection down on purpose, but rides
	// the same EventConnectionLost path as an unexpected drop. Without this guard
	// an auto-connect network would immediately reconnect after the user hit
	// Disconnect.
	if intentional {
		logger.Log.Debug().Int64("network_id", networkID).Msg("User-initiated disconnect, skipping auto-reconnect")
		return
	}

	// Atomically check and set reconnecting flag to prevent race conditions
	a.mu.Lock()
	if a.reconnectingNetworks[networkID] {
		logger.Log.Debug().Int64("network_id", networkID).Msg("Already reconnecting, skipping")
		a.mu.Unlock()
		return
	}

	if existingClient, exists := a.ircClients[networkID]; exists {
		if existingClient.IsConnectedDirect() {
			logger.Log.Debug().Int64("network_id", networkID).Msg("Already connected, skipping reconnect")
			a.mu.Unlock()
			return
		}
	}

	a.reconnectingNetworks[networkID] = true
	a.mu.Unlock()

	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		logger.Log.Debug().Err(err).Int64("network_id", networkID).Msg("Failed to get network for reconnect check")
		a.mu.Lock()
		delete(a.reconnectingNetworks, networkID)
		a.mu.Unlock()
		return
	}

	// An authentication failure rode the same EventConnectionLost path (the
	// client aborted with QUIT). Retrying a credential failure just loops, so
	// suppress auto-reconnect unless the user opted in.
	a.mu.Lock()
	client, hasClient := a.ircClients[networkID]
	a.mu.Unlock()
	if hasClient && client.AuthFailed() {
		setting, _ := a.GetSetting("reconnect_on_auth_failure")
		if authFailureBlocksReconnect(setting) {
			logger.Log.Info().Int64("network_id", networkID).Msg("Auth failure: suppressing auto-reconnect (reconnect_on_auth_failure is off)")
			a.mu.Lock()
			delete(a.reconnectingNetworks, networkID)
			a.mu.Unlock()
			return
		}
	}

	if !network.AutoConnect {
		logger.Log.Debug().Int64("network_id", networkID).Str("network", network.Name).Msg("Auto-connect disabled, skipping reconnect")
		// We are deliberately not reconnecting, but the dropped client's library
		// Loop will auto-reconnect on its own (~2 min) unless we set its quit flag.
		// Stop it so a network the user left disconnected can't silently come back
		// as a ghost session under a fallback nick.
		if hasClient && client != nil {
			client.StopReconnect()
		}
		a.mu.Lock()
		delete(a.reconnectingNetworks, networkID)
		a.mu.Unlock()
		return
	}

	go a.reconnectWithBackoff(networkID, network.Name)
}

// authFailureBlocksReconnect reports whether a connection dropped by an
// authentication failure should NOT be auto-reconnected. A credential failure
// never recovers by retrying, so the default (unset/empty) blocks reconnect.
// Users who want retries set reconnect_on_auth_failure to "true".
func authFailureBlocksReconnect(reconnectSetting string) bool {
	return reconnectSetting != "true"
}

// reconnectWithBackoff attempts to reconnect with exponential backoff
func (a *App) reconnectWithBackoff(networkID int64, networkName string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error().Interface("panic", r).Int64("network_id", networkID).Msg("PANIC in auto-reconnect")
		}
		a.mu.Lock()
		delete(a.reconnectingNetworks, networkID)
		a.mu.Unlock()
	}()

	logger.Log.Info().Int64("network_id", networkID).Str("network", networkName).Msg("Starting auto-reconnect")

	delay := 5 * time.Second
	maxDelay := 5 * time.Minute
	maxAttempts := 10

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			logger.Log.Info().
				Int64("network_id", networkID).
				Str("network", networkName).
				Int("attempt", attempt).
				Dur("delay", delay).
				Msg("Waiting before reconnect attempt")
			time.Sleep(delay)
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}

		// Check if network still exists and auto-connect is still enabled
		network, err := a.storage.GetNetwork(networkID)
		if err != nil || network == nil {
			logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Network not found, stopping reconnect")
			return
		}
		if !network.AutoConnect {
			logger.Log.Info().Int64("network_id", networkID).Str("network", network.Name).Msg("Auto-connect disabled, stopping reconnect")
			return
		}

		// Check if already connected
		a.mu.RLock()
		client, exists := a.ircClients[networkID]
		isConnected := exists && client != nil && client.IsConnectedDirect()
		a.mu.RUnlock()

		if isConnected {
			logger.Log.Info().Int64("network_id", networkID).Str("network", network.Name).Msg("Already connected, stopping reconnect")
			return
		}

		// Build config from network
		config, err := a.buildReconnectConfig(networkID, network)
		if err != nil {
			logger.Log.Error().Err(err).Int64("network_id", networkID).Msg("Failed to build reconnect config")
			continue
		}

		a.writeNetworkStatus(networkID, fmt.Sprintf("Attempting to reconnect (attempt %d/%d)...", attempt, maxAttempts))

		logger.Log.Info().
			Int64("network_id", networkID).
			Str("network", network.Name).
			Int("attempt", attempt).
			Msg("Attempting to reconnect")

		err = a.connectNetwork(config, true)
		if err != nil {
			logger.Log.Warn().
				Err(err).
				Int64("network_id", networkID).
				Str("network", network.Name).
				Int("attempt", attempt).
				Msg("Reconnect attempt failed")

			a.writeNetworkStatus(networkID, fmt.Sprintf("Reconnect attempt %d failed: %v", attempt, err))
			continue
		}

		// Success!
		logger.Log.Info().
			Int64("network_id", networkID).
			Str("network", network.Name).
			Int("attempt", attempt).
			Msg("Reconnected successfully")

		a.writeNetworkStatus(networkID, fmt.Sprintf("Reconnected successfully after %d attempt(s)", attempt))
		return
	}

	// Max attempts reached
	logger.Log.Error().
		Int64("network_id", networkID).
		Str("network", networkName).
		Int("max_attempts", maxAttempts).
		Msg("Max reconnect attempts reached, giving up")

	a.writeNetworkStatus(networkID, fmt.Sprintf("Failed to reconnect after %d attempts. Please reconnect manually.", maxAttempts))
}

// buildReconnectConfig builds a NetworkConfig from database state for reconnection
func (a *App) buildReconnectConfig(networkID int64, network *storage.Network) (NetworkConfig, error) {
	dbServers, err := a.storage.GetServers(networkID)
	if err != nil || len(dbServers) == 0 {
		return NetworkConfig{}, fmt.Errorf("failed to load servers for reconnect: %w", err)
	}

	serverConfigs := make([]ServerConfig, len(dbServers))
	for j, srv := range dbServers {
		serverConfigs[j] = ServerConfig{
			Address: srv.Address,
			Port:    srv.Port,
			TLS:     srv.TLS,
			Order:   srv.Order,
		}
	}

	config := NetworkConfig{
		Name:     network.Name,
		Nickname: network.Nickname,
		Username: network.Username,
		Realname: network.Realname,
		// Secrets are resolved from the keychain (falling back to any legacy
		// plaintext column). This config is used only for internal reconnects.
		Password:         a.creds.Resolve(networkID, security.FieldPassword, network.Password),
		SASLPassword:     a.creds.Resolve(networkID, security.FieldSASLPassword, derefStr(network.SASLPassword)),
		SASLExternalCert: derefStr(network.SASLExternalCert), // a path, not a secret
		SASLEnabled:      network.SASLEnabled,
		AutoConnect:      network.AutoConnect,
		IdentifyAsBot:    network.IdentifyAsBot,
		Servers:          serverConfigs,
	}

	if network.SASLMechanism != nil {
		config.SASLMechanism = *network.SASLMechanism
	}
	if network.SASLUsername != nil {
		config.SASLUsername = *network.SASLUsername
	}

	return config, nil
}

// autoConnect connects networks that have auto_connect enabled during startup
func (a *App) autoConnect(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error().Interface("panic", r).Msg("PANIC in auto-connect goroutine")
		}
	}()

	logger.Log.Info().Msg("Auto-connect goroutine started, waiting for initialization...")
	time.Sleep(constants.AutoConnectDelay)
	logger.Log.Info().Msg("Starting auto-connect process...")

	if a.storage == nil {
		logger.Log.Error().Msg("Storage is nil in auto-connect goroutine")
		return
	}

	networks, err := a.storage.GetNetworks()
	if err != nil {
		logger.Log.Error().Err(err).Msg("Failed to load networks for auto-connect")
		return
	}

	logger.Log.Info().Int("count", len(networks)).Msg("Found networks, checking for auto-connect...")

	for _, n := range networks {
		logger.Log.Info().Str("network", n.Name).Int64("id", n.ID).Bool("auto_connect", n.AutoConnect).Msg("Network status")
	}

	// Count networks with auto-connect enabled
	autoConnectCount := 0
	for _, network := range networks {
		if network.AutoConnect {
			autoConnectCount++
		}
	}

	logger.Log.Info().Int("count", autoConnectCount).Msg("Found networks with auto-connect enabled")
	if autoConnectCount == 0 {
		logger.Log.Info().Msg("No networks with auto-connect enabled, exiting")
		return
	}

	autoConnectIndex := 0
	for _, network := range networks {
		if network.AutoConnect {
			networkID := network.ID
			networkName := network.Name
			delay := time.Duration(autoConnectIndex) * constants.ConnectionStaggerDelay
			autoConnectIndex++

			logger.Log.Info().Str("network", networkName).Int64("id", networkID).Dur("delay", delay).Msg("Scheduling auto-connect for network")

			a.startupWg.Add(1)
			go func(nID int64, nName string, d time.Duration) {
				defer a.startupWg.Done()
				defer func() {
					if r := recover(); r != nil {
						logger.Log.Error().Interface("panic", r).Str("network", nName).Msg("PANIC in auto-connect")
					}
				}()

				select {
				case <-ctx.Done():
					logger.Log.Debug().Str("network", nName).Msg("Startup cancelled, skipping auto-connect")
					return
				case <-time.After(d):
				}

				logger.Log.Info().Str("network", nName).Int64("id", nID).Msg("Starting auto-connect for network")

				dbServers, err := a.storage.GetServers(nID)
				if err != nil || len(dbServers) == 0 {
					logger.Log.Error().Err(err).Str("network", nName).Msg("Failed to load servers")
					return
				}

				// Reload network to ensure we have latest data
				networks, err := a.storage.GetNetworks()
				if err != nil {
					logger.Log.Error().Err(err).Msg("Failed to reload networks")
					return
				}

				var network *storage.Network
				for i := range networks {
					if networks[i].ID == nID {
						network = &networks[i]
						break
					}
				}

				if network == nil {
					logger.Log.Warn().Str("network", nName).Int64("id", nID).Msg("Network not found in database")
					return
				}

				if !network.AutoConnect {
					logger.Log.Info().Str("network", nName).Int64("id", nID).Msg("Network no longer has auto-connect enabled, skipping")
					return
				}

				// Build config from network and servers
				serverConfigs := make([]ServerConfig, len(dbServers))
				for j, srv := range dbServers {
					serverConfigs[j] = ServerConfig{
						Address: srv.Address,
						Port:    srv.Port,
						TLS:     srv.TLS,
						Order:   srv.Order,
					}
				}
				config := NetworkConfig{
					Servers:  serverConfigs,
					Name:     network.Name,
					Nickname: network.Nickname,
					Username: network.Username,
					Realname: network.Realname,
					// Resolve secrets from the keychain (falling back to any legacy
					// plaintext column) for the auto-connect.
					Password:         a.creds.Resolve(network.ID, security.FieldPassword, network.Password),
					SASLPassword:     a.creds.Resolve(network.ID, security.FieldSASLPassword, derefStr(network.SASLPassword)),
					SASLExternalCert: derefStr(network.SASLExternalCert), // a path, not a secret
					SASLEnabled:      network.SASLEnabled,
					AutoConnect:      network.AutoConnect,
					IdentifyAsBot:    network.IdentifyAsBot,
				}
				if network.SASLMechanism != nil {
					config.SASLMechanism = *network.SASLMechanism
				}
				if network.SASLUsername != nil {
					config.SASLUsername = *network.SASLUsername
				}

				logger.Log.Debug().
					Str("network", nName).
					Str("config_name", config.Name).
					Int("servers", len(config.Servers)).
					Bool("auto_connect", config.AutoConnect).
					Msg("Calling ConnectNetwork")

				if err := a.ConnectNetwork(config); err != nil {
					logger.Log.Error().Err(err).Str("network", nName).Str("error_type", fmt.Sprintf("%T", err)).Msg("Failed to auto-connect")
				} else {
					logger.Log.Info().Str("network", nName).Msg("Auto-connected successfully")
					time.Sleep(constants.AutoJoinDelay)
					connected, statusErr := a.GetConnectionStatus(nID)
					if statusErr != nil {
						logger.Log.Warn().Err(statusErr).Str("network", nName).Msg("Could not verify connection status")
					} else {
						logger.Log.Debug().Str("network", nName).Bool("connected", connected).Msg("Connection status")
					}
				}
			}(networkID, networkName, delay)
		}
	}
}

// ConnectSavedNetwork connects to a saved network by ID, building its config
// from stored state. Used by the deep-link flow when a link targets a saved but
// currently-disconnected network. No-op-safe to call; connectNetwork handles an
// already-connected client.
func (a *App) ConnectSavedNetwork(networkID int64) error {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return fmt.Errorf("connect saved network %d: %w", networkID, err)
	}
	config, err := a.buildReconnectConfig(networkID, network)
	if err != nil {
		return fmt.Errorf("connect saved network %d: %w", networkID, err)
	}
	return a.connectNetwork(config, false)
}

// GetConnectionStatus returns whether a network is connected
func (a *App) GetConnectionStatus(networkID int64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	client, exists := a.ircClients[networkID]
	if !exists {
		return false, nil
	}

	return client.IsConnected(), nil
}

// wakeReconnectClient is the slice of IRCClient behavior reconnectAllOnWake needs.
// Extracting it makes the wake decision — probe first, reconnect only the dead —
// unit-testable without a live socket (see app_connection_wake_test.go).
type wakeReconnectClient interface {
	IsConnected() bool
	ProbeAlive(deadline time.Duration) bool
	ForceReconnect()
}

// reconnectDeadOnWake probes each still-"connected" client and force-reconnects only
// those that fail to answer within deadline, all concurrently. A link macOS kept alive
// across sleep (TCPKeepAlive) answers its keepalive PING and is left untouched — the
// fix for maintenance-DarkWake churn, where the old unconditional ForceReconnect tore
// down healthy links and rejoined every channel (visible ##chat JOIN-spam). A client
// that already dropped (!IsConnected) is skipped and left to the normal
// handleConnectionLost path. Returns how many links it reconnected.
func reconnectDeadOnWake(clients []wakeReconnectClient, deadline time.Duration) int {
	var wg sync.WaitGroup
	var reconnected int32
	for _, c := range clients {
		if !c.IsConnected() {
			continue
		}
		wg.Add(1)
		go func(c wakeReconnectClient) {
			defer wg.Done()
			if !c.ProbeAlive(deadline) {
				c.ForceReconnect()
				atomic.AddInt32(&reconnected, 1)
			}
		}(c)
	}
	wg.Wait()
	return int(reconnected)
}

// reconnectAllOnWake recovers auto-connect networks after the system wakes. macOS
// keeps IRC sockets alive across sleep (TCPKeepAlive) and does frequent short
// maintenance DarkWakes, so this no longer force-reconnects blindly: it PINGs each
// still-connected link and reconnects only the ones that don't answer. Teardown of a
// dead link flows through the normal DisconnectCallback -> handleConnectionLost ->
// auto-reconnect path. Networks with auto-connect disabled are left untouched.
func (a *App) reconnectAllOnWake() {
	type entry struct {
		id     int64
		client *irc.IRCClient
	}
	a.mu.RLock()
	entries := make([]entry, 0, len(a.ircClients))
	for id, c := range a.ircClients {
		entries = append(entries, entry{id, c})
	}
	a.mu.RUnlock()

	eligible := make([]wakeReconnectClient, 0, len(entries))
	for _, e := range entries {
		network, err := a.storage.GetNetwork(e.id)
		if err != nil || network == nil || !network.AutoConnect {
			continue
		}
		eligible = append(eligible, e.client)
	}

	if n := reconnectDeadOnWake(eligible, constants.WakeProbeTimeout); n > 0 {
		logger.Log.Info().Int("reconnected", n).Int("checked", len(eligible)).Msg("Wake: reconnected links that failed the liveness probe")
	}
}

// GetCurrentNick returns the nick the server currently knows us by on a network,
// which can differ from the configured nick while a nick collision is being
// resolved. Returns an empty string when the network has no active client.
func (a *App) GetCurrentNick(networkID int64) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	client, exists := a.ircClients[networkID]
	if !exists {
		return "", nil
	}

	return client.CurrentNick(), nil
}

// DisconnectNetwork disconnects from a network
func (a *App) DisconnectNetwork(networkID int64) error {
	a.mu.Lock()

	client, exists := a.ircClients[networkID]
	if !exists {
		a.mu.Unlock()
		return fmt.Errorf("network not connected")
	}

	// Flag this as a deliberate teardown so the resulting EventConnectionLost
	// doesn't trip auto-reconnect (handleConnectionLost consumes the flag). Only
	// when actually connected: disconnecting an already-dead client fires no
	// DisconnectCallback, so the flag would otherwise leak onto the next genuine
	// drop and wrongly suppress that reconnect. Set before the (lock-free)
	// Disconnect so it is in place before the teardown fires the callback.
	if client.IsConnectedDirect() {
		a.intentionalDisconnect[networkID] = true
	}
	a.mu.Unlock()

	// Disconnect blocks until the library Loop goroutine exits (bounded), so it
	// must not run under a.mu — that lock guards every network's state.
	if err := client.Disconnect(); err != nil {
		a.mu.Lock()
		delete(a.intentionalDisconnect, networkID)
		a.mu.Unlock()
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	a.mu.Lock()
	delete(a.ircClients, networkID)
	a.mu.Unlock()
	return nil
}

// DeleteNetwork deletes a network configuration
func (a *App) DeleteNetwork(networkID int64) error {
	a.mu.Lock()
	client, exists := a.ircClients[networkID]
	if exists {
		delete(a.ircClients, networkID)
		// Flag the teardown as deliberate, exactly like DisconnectNetwork: the
		// Disconnect below fires EventConnectionLost, and without this marker
		// handleConnectionLost treats it as an unexpected drop and reconnects —
		// racing the row deletion and resurrecting the network via
		// buildNetworkFromConfig's create-if-missing branch. A leaked marker is
		// inert: the id is never reused (AUTOINCREMENT) once the row is gone.
		a.intentionalDisconnect[networkID] = true
	}
	a.mu.Unlock()

	// Stop the client outside a.mu: Disconnect waits (bounded) for the library
	// Loop goroutine to exit, and that lock guards every network's state.
	if exists && client != nil {
		client.Disconnect()
	}

	if err := a.storage.DeleteNetwork(networkID); err != nil {
		return err
	}
	_ = os.Remove(a.networkIconPath(networkID)) // best-effort icon cleanup
	// Remove any secrets held in the keychain for this network.
	if err := a.creds.Delete(networkID); err != nil {
		logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Failed to delete network secrets from keychain")
	}
	a.emit("networks:changed")
	return nil
}

// SetNetworkColor sets the rail tile color palette key. Empty string clears it
// (NULL), reverting to the deterministic name-hashed fallback.
func (a *App) SetNetworkColor(networkID int64, color string) error {
	var c *string
	if color != "" {
		c = &color
	}
	if err := a.storage.UpdateNetworkColor(networkID, c); err != nil {
		return fmt.Errorf("failed to set network color: %w", err)
	}
	return nil
}

// ReorderNetworks persists the rail order; each id's sort_order becomes its 1-based index.
func (a *App) ReorderNetworks(orderedIDs []int64) error {
	for i, id := range orderedIDs {
		if err := a.storage.UpdateNetworkSortOrder(id, int64(i+1)); err != nil {
			return fmt.Errorf("failed to set sort_order for network %d: %w", id, err)
		}
	}
	return nil
}

// networkIconPath returns the on-disk path for a network's processed icon.
func (a *App) networkIconPath(networkID int64) string {
	return filepath.Join(a.dataDir, "network-icons", fmt.Sprintf("%d.png", networkID))
}

// SetNetworkIcon processes a base64-encoded uploaded image into a 128x128 PNG,
// writes it under <dataDir>/network-icons/<id>.png, and records the path.
func (a *App) SetNetworkIcon(networkID int64, dataB64 string) error {
	raw, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	pngBytes, err := imageproc.SquareIconPNG(raw, 128)
	if err != nil {
		return fmt.Errorf("process icon: %w", err)
	}
	path := a.networkIconPath(networkID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir icons: %w", err)
	}
	if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
		return fmt.Errorf("write icon: %w", err)
	}
	if err := a.storage.UpdateNetworkIcon(networkID, path); err != nil {
		return fmt.Errorf("persist icon path: %w", err)
	}
	return nil
}

// RemoveNetworkIcon deletes the icon file and clears the stored path.
func (a *App) RemoveNetworkIcon(networkID int64) error {
	_ = os.Remove(a.networkIconPath(networkID)) // best-effort
	if err := a.storage.ClearNetworkIcon(networkID); err != nil {
		return fmt.Errorf("clear icon path: %w", err)
	}
	return nil
}

// GetNetworkIcon returns the network's icon as a data URL, or "" if none.
func (a *App) GetNetworkIcon(networkID int64) (string, error) {
	path := a.networkIconPath(networkID)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read icon: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b), nil
}

// ToggleNetworkAutoConnect toggles the auto-connect setting for a network
func (a *App) ToggleNetworkAutoConnect(networkID int64) error {
	networks, err := a.storage.GetNetworks()
	if err != nil {
		return fmt.Errorf("failed to get networks: %w", err)
	}

	var network *storage.Network
	for i := range networks {
		if networks[i].ID == networkID {
			network = &networks[i]
			break
		}
	}

	if network == nil {
		return fmt.Errorf("network not found")
	}

	newAutoConnect := !network.AutoConnect
	logger.Log.Info().
		Str("network", network.Name).
		Int64("id", networkID).
		Bool("old_value", network.AutoConnect).
		Bool("new_value", newAutoConnect).
		Msg("Toggling auto-connect for network")

	if err := a.storage.UpdateNetworkAutoConnect(networkID, newAutoConnect); err != nil {
		return err
	}
	a.emit("networks:changed")
	return nil
}

// findNetworkIDByAddress looks up a network ID from a server address
func (a *App) findNetworkIDByAddress(address string) (int64, bool) {
	networks, err := a.storage.GetNetworks()
	if err != nil {
		return 0, false
	}

	for _, n := range networks {
		if n.Address == address {
			return n.ID, true
		}
		servers, err := a.storage.GetServers(n.ID)
		if err == nil {
			for _, srv := range servers {
				if srv.Address == address {
					return n.ID, true
				}
			}
		}
	}
	return 0, false
}

// findNetworksByAddress returns the IDs of every saved network whose primary
// address or any configured server address equals host. Unlike
// findNetworkIDByAddress (which returns the first match), this surfaces all
// matches so a deep link to a host with multiple saved identities can be
// disambiguated.
func (a *App) findNetworksByAddress(host string) []int64 {
	networks, err := a.storage.GetNetworks()
	if err != nil {
		return nil
	}
	var ids []int64
	for _, n := range networks {
		if n.Address == host {
			ids = append(ids, n.ID)
			continue
		}
		servers, err := a.storage.GetServers(n.ID)
		if err != nil {
			continue
		}
		for _, srv := range servers {
			if srv.Address == host {
				ids = append(ids, n.ID)
				break
			}
		}
	}
	return ids
}

// resolveNetworkID extracts network ID from event data, trying direct ID first, then address lookup
func (a *App) resolveNetworkID(data map[string]interface{}) (int64, bool) {
	// First try to get networkId directly from event data
	if networkIDVal, ok := data["networkId"]; ok {
		switch v := networkIDVal.(type) {
		case int64:
			return v, true
		case int:
			return int64(v), true
		case float64:
			return int64(v), true
		}
	}

	// Fall back to address-based lookup
	if networkAddress, ok := data["network"].(string); ok {
		return a.findNetworkIDByAddress(networkAddress)
	}

	return 0, false
}

// writeShutdownDisconnectMessages gracefully tears down every live connection during
// app shutdown: it records "Disconnected from server" on each network's status window
// and sends a QUIT so the server sees a clean part (which peers observe as our QUIT
// message). No per-channel "Disconnected" line is written — the QUIT/JOIN protocol
// messages the server echoes are the source of truth for channel scrollback.
func (a *App) writeShutdownDisconnectMessages() {
	a.mu.Lock()
	clients := make(map[int64]*irc.IRCClient)
	for networkID, client := range a.ircClients {
		clients[networkID] = client
	}
	a.mu.Unlock()

	logger.Log.Info().Int("client_count", len(clients)).Msg("Found IRC clients to check during shutdown")

	for networkID, client := range clients {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Log.Error().Interface("panic", r).Int64("network_id", networkID).Msg("Panic during shutdown for network")
				}
			}()

			// The app is quitting: every drop this teardown causes is deliberate.
			// Disconnect() rides the same EventConnectionLost path as an unexpected
			// drop, so without the marker handleConnectionLost auto-reconnects every
			// auto-connect network FROM THE DYING PROCESS — zombie sessions that race
			// the next app instance for the user's nick. Marked for every client
			// (connected or not): a half-open client's late drop must not reconnect
			// during shutdown either.
			a.mu.Lock()
			a.intentionalDisconnect[networkID] = true
			a.mu.Unlock()

			if !client.IsConnectedDirect() {
				return
			}

			client.WriteStatusMessage("Disconnected from server")

			time.Sleep(100 * time.Millisecond)

			logger.Log.Debug().Int64("network_id", networkID).Msg("Disconnecting from network (sending QUIT command)")
			if err := client.Disconnect(); err != nil {
				logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Error disconnecting from network")
			} else {
				logger.Log.Info().Int64("network_id", networkID).Msg("Disconnected from network")
			}
		}()
	}
	logger.Log.Debug().Msg("Finished processing all networks during shutdown")
}
