package main

import (
	"context"
	"fmt"
	"time"

	"github.com/matt0x6f/irc-client/internal/constants"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
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

	if network == nil {
		// Create new network record
		network = &storage.Network{
			Name:             config.Name,
			Address:          servers[0].Address,
			Port:             servers[0].Port,
			TLS:              servers[0].TLS,
			Nickname:         config.Nickname,
			Username:         config.Username,
			Realname:         config.Realname,
			Password:         config.Password,
			SASLEnabled:      config.SASLEnabled,
			SASLMechanism:    stringPtr(config.SASLMechanism),
			SASLUsername:     stringPtr(config.SASLUsername),
			SASLPassword:     stringPtr(config.SASLPassword),
			SASLExternalCert: stringPtr(config.SASLExternalCert),
			AutoConnect:      persistAutoConnect && config.AutoConnect,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}

		if err := a.storage.CreateNetwork(network); err != nil {
			return nil, fmt.Errorf("failed to create network: %w", err)
		}
	} else {
		// Update existing network config
		network.Name = config.Name
		network.Nickname = config.Nickname
		network.Username = config.Username
		network.Realname = config.Realname
		network.Password = config.Password
		network.SASLEnabled = config.SASLEnabled
		network.SASLMechanism = stringPtr(config.SASLMechanism)
		network.SASLUsername = stringPtr(config.SASLUsername)
		network.SASLPassword = stringPtr(config.SASLPassword)
		network.SASLExternalCert = stringPtr(config.SASLExternalCert)
		// Only an explicit user edit may change the auto_connect preference;
		// a connect operation must preserve the stored value.
		if persistAutoConnect {
			network.AutoConnect = config.AutoConnect
		}
		network.UpdatedAt = time.Now()
		if err := a.storage.UpdateNetwork(network); err != nil {
			return nil, fmt.Errorf("failed to update network: %w", err)
		}
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

	// Connecting must never mutate the stored auto_connect preference.
	network, err := a.buildNetworkFromConfig(config, servers, false)
	if err != nil {
		return err
	}

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
	if existingClient, exists := a.ircClients[network.ID]; exists {
		if existingClient.IsConnectedDirect() {
			logger.Log.Warn().Str("network", config.Name).Int64("id", network.ID).Msg("Already connected, rejecting duplicate connection")
			a.mu.Unlock()
			return fmt.Errorf("already connected to %s", config.Name)
		}
		logger.Log.Debug().Str("network", config.Name).Int64("id", network.ID).Msg("Cleaning up disconnected client before reconnecting")
		delete(a.ircClients, network.ID)
	}

	// Mark that we're connecting to this network BEFORE releasing the lock
	connectDone := make(chan struct{})
	a.connectingNetworks[networkKey] = connectDone
	logger.Log.Debug().Str("network_key", networkKey).Str("network", config.Name).Int64("id", network.ID).Msg("Marked as connecting")

	a.mu.Unlock()

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

	// Try connecting to servers in order
	var lastErr error
	for i, srv := range dbServers {
		// Create a temporary network object with this server's address
		tempNetwork := *network
		tempNetwork.Address = srv.Address
		tempNetwork.Port = srv.Port
		tempNetwork.TLS = srv.TLS

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
		a.storage.WriteMessage(storage.Message{
			NetworkID:   network.ID,
			ChannelID:   nil,
			User:        "*",
			Message:     connectingMsg,
			MessageType: "status",
			Timestamp:   time.Now(),
		})

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

			a.storage.WriteMessage(storage.Message{
				NetworkID:   network.ID,
				ChannelID:   nil,
				User:        "*",
				Message:     fmt.Sprintf("Failed to connect to %s:%d: %v", srv.Address, srv.Port, connErr),
				MessageType: "status",
				Timestamp:   time.Now(),
			})

			continue
		}

		// Success! Store client and clear connection-in-progress flag
		logger.Log.Info().Str("server", serverKey).Int64("network_id", network.ID).Msg("Connection successful")
		a.mu.Lock()

		// Final check: if another connection completed and stored a client, disconnect this one
		if existingClient, exists := a.ircClients[network.ID]; exists && existingClient != ircClient {
			if existingClient.IsConnectedDirect() {
				logger.Log.Warn().Str("network", config.Name).Int64("id", network.ID).Msg("Another connection completed first, disconnecting duplicate")
				ircClient.Disconnect()
				delete(a.connectingNetworks, networkKey)
				close(connectDone)
				a.mu.Unlock()
				return fmt.Errorf("connection to %s already established by another process", config.Name)
			}
		}

		a.ircClients[network.ID] = ircClient
		delete(a.connectingNetworks, networkKey)
		delete(a.reconnectingNetworks, network.ID)
		close(connectDone)
		a.mu.Unlock()

		a.storage.WriteMessage(storage.Message{
			NetworkID:   network.ID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Connected to %s:%d", srv.Address, srv.Port),
			MessageType: "status",
			Timestamp:   time.Now(),
		})

		// If this connect closed an open disconnect→reconnect gap, bracket it in the
		// channel/PM buffers with a "Reconnected" marker. Gating on the gap flag (not
		// the reconnect arg) means a manual reconnect after a drop still closes the
		// bracket, while a true first-time connect (no gap) writes nothing.
		a.closeConnectionGap(network.ID, network)

		return nil
	}

	// All servers failed
	logger.Log.Error().Str("network", config.Name).Msg("All connection attempts failed")
	a.mu.Lock()
	delete(a.connectingNetworks, networkKey)
	close(connectDone)
	a.mu.Unlock()

	a.storage.WriteMessage(storage.Message{
		NetworkID:   network.ID,
		ChannelID:   nil,
		User:        "*",
		Message:     fmt.Sprintf("Failed to connect to any server: %v", lastErr),
		MessageType: "status",
		Timestamp:   time.Now(),
	})

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
		a.storage.WriteMessage(storage.Message{
			NetworkID:   networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("STS upgrade to TLS failed: %v", err),
			MessageType: "status",
			Timestamp:   time.Now(),
		})
	}
}

// handleConnectionLost handles auto-reconnect when a connection is lost
func (a *App) handleConnectionLost(networkID int64) {
	// A deliberate STS plaintext→TLS upgrade tears down the old connection on
	// purpose; don't let the auto-reconnect path race the upgrade's own reconnect.
	a.mu.RLock()
	upgrading := a.stsUpgrading[networkID]
	a.mu.RUnlock()
	if upgrading {
		logger.Log.Debug().Int64("network_id", networkID).Msg("STS upgrade in progress, skipping auto-reconnect")
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

	// Mark the drop point in each open channel/PM buffer. Done here (a confirmed
	// disconnect, past the IsConnectedDirect false-positive guard) but before the
	// auto-connect gate, so the gap is delineated even when auto-reconnect is
	// disabled and no "Reconnected" marker will ever follow.
	a.openConnectionGap(networkID, network)

	if !network.AutoConnect {
		logger.Log.Debug().Int64("network_id", networkID).Str("network", network.Name).Msg("Auto-connect disabled, skipping reconnect")
		a.mu.Lock()
		delete(a.reconnectingNetworks, networkID)
		a.mu.Unlock()
		return
	}

	go a.reconnectWithBackoff(networkID, network.Name)
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

		a.storage.WriteMessage(storage.Message{
			NetworkID:   networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Attempting to reconnect (attempt %d/%d)...", attempt, maxAttempts),
			MessageType: "status",
			Timestamp:   time.Now(),
		})

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

			a.storage.WriteMessage(storage.Message{
				NetworkID:   networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     fmt.Sprintf("Reconnect attempt %d failed: %v", attempt, err),
				MessageType: "status",
				Timestamp:   time.Now(),
			})
			continue
		}

		// Success!
		logger.Log.Info().
			Int64("network_id", networkID).
			Str("network", network.Name).
			Int("attempt", attempt).
			Msg("Reconnected successfully")

		a.storage.WriteMessage(storage.Message{
			NetworkID:   networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Reconnected successfully after %d attempt(s)", attempt),
			MessageType: "status",
			Timestamp:   time.Now(),
		})
		return
	}

	// Max attempts reached
	logger.Log.Error().
		Int64("network_id", networkID).
		Str("network", networkName).
		Int("max_attempts", maxAttempts).
		Msg("Max reconnect attempts reached, giving up")

	a.storage.WriteMessage(storage.Message{
		NetworkID:   networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     fmt.Sprintf("Failed to reconnect after %d attempts. Please reconnect manually.", maxAttempts),
		MessageType: "status",
		Timestamp:   time.Now(),
	})
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
		Name:        network.Name,
		Nickname:    network.Nickname,
		Username:    network.Username,
		Realname:    network.Realname,
		Password:    network.Password,
		SASLEnabled: network.SASLEnabled,
		AutoConnect: network.AutoConnect,
		Servers:     serverConfigs,
	}

	if network.SASLMechanism != nil {
		config.SASLMechanism = *network.SASLMechanism
	}
	if network.SASLUsername != nil {
		config.SASLUsername = *network.SASLUsername
	}
	if network.SASLPassword != nil {
		config.SASLPassword = *network.SASLPassword
	}
	if network.SASLExternalCert != nil {
		config.SASLExternalCert = *network.SASLExternalCert
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
					Servers:     serverConfigs,
					Name:        network.Name,
					Nickname:    network.Nickname,
					Username:    network.Username,
					Realname:    network.Realname,
					Password:    network.Password,
					SASLEnabled: network.SASLEnabled,
					AutoConnect: network.AutoConnect,
				}
				if network.SASLMechanism != nil {
					config.SASLMechanism = *network.SASLMechanism
				}
				if network.SASLUsername != nil {
					config.SASLUsername = *network.SASLUsername
				}
				if network.SASLPassword != nil {
					config.SASLPassword = *network.SASLPassword
				}
				if network.SASLExternalCert != nil {
					config.SASLExternalCert = *network.SASLExternalCert
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

// reconnectAllOnWake forces every auto-connect network to reconnect after the
// system wakes from sleep, where sockets are usually dead but not yet detected by
// the library's ping loop. Teardown flows through the normal DisconnectCallback ->
// handleConnectionLost -> auto-reconnect path. Networks with auto-connect disabled
// are left untouched so a manually-managed connection that survived a brief sleep
// is not needlessly killed; a genuinely dead one there is still caught by the
// library ping loop within KeepAlive+Timeout.
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

	for _, e := range entries {
		if !e.client.IsConnected() {
			continue
		}
		network, err := a.storage.GetNetwork(e.id)
		if err != nil || network == nil || !network.AutoConnect {
			continue
		}
		e.client.ForceReconnect()
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
	defer a.mu.Unlock()

	client, exists := a.ircClients[networkID]
	if !exists {
		return fmt.Errorf("network not connected")
	}

	if err := client.Disconnect(); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	delete(a.ircClients, networkID)
	return nil
}

// DeleteNetwork deletes a network configuration
func (a *App) DeleteNetwork(networkID int64) error {
	a.mu.Lock()
	client, exists := a.ircClients[networkID]
	if exists {
		client.Disconnect()
		delete(a.ircClients, networkID)
	}
	delete(a.connectionGapOpen, networkID)
	a.mu.Unlock()

	if err := a.storage.DeleteNetwork(networkID); err != nil {
		return err
	}
	a.emit("networks:changed")
	return nil
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

// writeShutdownDisconnectMessages writes "Disconnected" messages to all open channels during shutdown
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

			logger.Log.Debug().Int64("network_id", networkID).Msg("Checking network for disconnect messages")
			isConnected := client.IsConnectedDirect()

			// Get network info with timeout
			network := a.getNetworkWithTimeout(networkID)

			if network != nil {
				a.writeDisconnectToChannels(networkID, network)
			}

			if isConnected {
				client.WriteStatusMessage("Disconnected from server")
			}

			time.Sleep(100 * time.Millisecond)

			if isConnected {
				logger.Log.Debug().Int64("network_id", networkID).Msg("Disconnecting from network (sending QUIT command)")
				if err := client.Disconnect(); err != nil {
					logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Error disconnecting from network")
				} else {
					logger.Log.Info().Int64("network_id", networkID).Msg("Disconnected from network")
				}
			}
		}()
	}
	logger.Log.Debug().Msg("Finished processing all networks during shutdown")
}

// getNetworkWithTimeout retrieves a network from storage with a 1-second timeout
func (a *App) getNetworkWithTimeout(networkID int64) *storage.Network {
	networkChan := make(chan *storage.Network, 1)
	errChan := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				errChan <- fmt.Errorf("panic in GetNetwork: %v", r)
			}
		}()
		net, err := a.storage.GetNetwork(networkID)
		if err != nil {
			errChan <- err
		} else {
			networkChan <- net
		}
	}()

	select {
	case network := <-networkChan:
		return network
	case err := <-errChan:
		logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Failed to get network for disconnect message")
		return nil
	case <-time.After(1 * time.Second):
		logger.Log.Warn().Int64("network_id", networkID).Msg("GetNetwork timed out")
		return nil
	}
}

// openBufferChannels returns the channels whose buffers are currently "live" for a
// network: those flagged open plus those we are joined to, deduped. It does not
// include the all-channels shutdown fallback — callers that need that handle it.
func (a *App) openBufferChannels(networkID int64, network *storage.Network) []storage.Channel {
	allChannels, err := a.storage.GetChannels(networkID)
	if err != nil {
		logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Failed to get channels for connection buffer enumeration")
		return nil
	}

	var channels []storage.Channel
	seen := make(map[int64]bool)

	// Channels with an open pane.
	for _, ch := range allChannels {
		if ch.IsOpen {
			channels = append(channels, ch)
			seen[ch.ID] = true
		}
	}

	// Plus channels we are currently joined to.
	if network.Nickname != "" {
		joinedChannels, err := a.storage.GetJoinedChannels(networkID, network.Nickname)
		if err == nil {
			for _, ch := range joinedChannels {
				if !seen[ch.ID] {
					channels = append(channels, ch)
					seen[ch.ID] = true
				}
			}
		}
	}

	return channels
}

// writeDisconnectToChannels writes "Disconnected" messages to all open/joined channels
func (a *App) writeDisconnectToChannels(networkID int64, network *storage.Network) {
	channelsToNotify := a.openBufferChannels(networkID, network)

	// Fallback: write to all channels if no open/joined channels found
	if len(channelsToNotify) == 0 {
		allChannels, err := a.storage.GetChannels(networkID)
		if err == nil && len(allChannels) > 0 {
			logger.Log.Info().Int64("network_id", networkID).Int("total_channels", len(allChannels)).Msg("No open/joined channels found, writing disconnect messages to all channels as fallback")
			channelsToNotify = allChannels
		}
	}

	for _, channel := range channelsToNotify {
		channelID := channel.ID
		disconnectMsg := storage.Message{
			NetworkID:   networkID,
			ChannelID:   &channelID,
			User:        "*",
			Message:     "Disconnected",
			MessageType: "status",
			Timestamp:   time.Now(),
		}
		if err := a.storage.WriteMessageDirect(disconnectMsg); err != nil {
			if err.Error() != "storage is closed" {
				logger.Log.Warn().Err(err).Int64("network_id", networkID).Str("channel", channel.Name).Msg("Failed to write disconnect message to channel")
			}
		}
	}
}

// openConnectionGap records the start of a disconnect→reconnect gap and writes a
// "Disconnected" marker into each open channel/PM buffer. Idempotent: a gap that is
// already open is left untouched, so repeated connection.lost events for the same
// drop produce only a single marker.
func (a *App) openConnectionGap(networkID int64, network *storage.Network) {
	a.mu.Lock()
	alreadyOpen := a.connectionGapOpen[networkID]
	if !alreadyOpen {
		a.connectionGapOpen[networkID] = true
	}
	a.mu.Unlock()

	if !alreadyOpen {
		a.writeConnectionMarker(networkID, network, "Disconnected")
	}
}

// closeConnectionGap closes an open disconnect→reconnect gap, writing a "Reconnected"
// marker into each open channel/PM buffer. A no-op when no gap is open (e.g. a
// first-time connect), so only genuine recoveries are bracketed.
func (a *App) closeConnectionGap(networkID int64, network *storage.Network) {
	a.mu.Lock()
	wasOpen := a.connectionGapOpen[networkID]
	delete(a.connectionGapOpen, networkID)
	a.mu.Unlock()

	if wasOpen {
		a.writeConnectionMarker(networkID, network, "Reconnected")
	}
}

// writeConnectionMarker writes a delineation marker (label "Disconnected" or
// "Reconnected") into every currently-open channel and PM buffer for a network, so
// scrollback shows where the connection broke and where it recovered. Markers are
// stored as message_type "marker"; the frontend renders them as a centered divider.
//
// Each buffered message gets its own channelID local because WriteMessage queues the
// struct (with its *int64 pointer) for a later flush — sharing &channel.ID across the
// loop would make every flushed marker resolve to the last channel's id.
func (a *App) writeConnectionMarker(networkID int64, network *storage.Network, label string) {
	for _, channel := range a.openBufferChannels(networkID, network) {
		channelID := channel.ID
		marker := storage.Message{
			NetworkID:   networkID,
			ChannelID:   &channelID,
			User:        "*",
			Message:     label,
			MessageType: "marker",
			Timestamp:   time.Now(),
		}
		if err := a.storage.WriteMessage(marker); err != nil {
			logger.Log.Warn().Err(err).Int64("network_id", networkID).Str("channel", channel.Name).Str("label", label).Msg("Failed to write connection marker to channel")
		}
	}

	pmConvs, err := a.storage.GetOpenPMConversations(networkID, network.Nickname)
	if err != nil {
		logger.Log.Warn().Err(err).Int64("network_id", networkID).Msg("Failed to get open PM conversations for connection marker")
		return
	}
	for _, conv := range pmConvs {
		marker := storage.Message{
			NetworkID:   networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     label,
			MessageType: "marker",
			Timestamp:   time.Now(),
			PMTarget:    conv.TargetUser,
		}
		if err := a.storage.WriteMessage(marker); err != nil {
			logger.Log.Warn().Err(err).Int64("network_id", networkID).Str("pm_target", conv.TargetUser).Str("label", label).Msg("Failed to write connection marker to PM")
		}
	}
}
