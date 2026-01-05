package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/constants"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/plugin"
	"github.com/matt0x6f/irc-client/internal/security"
	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/matt0x6f/irc-client/internal/validation"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx                context.Context
	storage            *storage.Storage
	eventBus           *events.EventBus
	pluginManager      *plugin.Manager
	keychain           *security.Keychain
	ircClients         map[int64]*irc.IRCClient
	connectingNetworks map[string]chan struct{} // Track networks currently connecting by "address:port"
	mu                 sync.RWMutex
	startupCtx         context.Context
	startupCancel      context.CancelFunc
	startupWg          sync.WaitGroup
}

// NewApp creates a new App application struct
func NewApp() (*App, error) {
	// Get database path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	dbPath := filepath.Join(homeDir, ".cascade-chat", "cascade-chat.db")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create storage
	stor, err := storage.NewStorage(dbPath, 100, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create event bus
	eventBus := events.NewEventBus()

	// Create keychain for secure password storage
	keychain := security.NewKeychain()

	// Create plugin manager
	pluginDir := filepath.Join(homeDir, ".cascade-chat", "plugins")
	pluginMgr := plugin.NewManager(eventBus, pluginDir)

	// Discover and load plugins
	if err := pluginMgr.DiscoverAndLoad(); err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to load plugins")
	}

	app := &App{
		storage:            stor,
		eventBus:           eventBus,
		pluginManager:      pluginMgr,
		keychain:           keychain,
		ircClients:         make(map[int64]*irc.IRCClient),
		connectingNetworks: make(map[string]chan struct{}),
	}

	// Subscribe to message events to forward to frontend
	eventBus.Subscribe(irc.EventMessageSent, app)
	eventBus.Subscribe(irc.EventMessageReceived, app)
	// Subscribe to connection status events
	eventBus.Subscribe(irc.EventConnectionEstablished, app)
	eventBus.Subscribe(irc.EventConnectionLost, app)
	// Subscribe to channel list change events
	eventBus.Subscribe(irc.EventChannelsChanged, app)
	// Subscribe to user events to forward to frontend
	eventBus.Subscribe(irc.EventUserJoined, app)
	eventBus.Subscribe(irc.EventUserParted, app)
	eventBus.Subscribe(irc.EventUserQuit, app)
	eventBus.Subscribe(irc.EventUserKicked, app)
	eventBus.Subscribe(irc.EventUserNick, app)

	// Start processing plugin actions
	go app.processPluginActions()

	return app, nil
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Create context for startup goroutines with cancellation
	a.startupCtx, a.startupCancel = context.WithCancel(context.Background())
	logger.Log.Info().Msg("App startup function called")

	// Auto-connect networks that have auto_connect enabled
	a.startupWg.Add(1)
	go func() {
		defer a.startupWg.Done()
		defer func() {
			if r := recover(); r != nil {
				logger.Log.Error().Interface("panic", r).Msg("PANIC in auto-connect goroutine")
			}
		}()

		logger.Log.Info().Msg("Auto-connect goroutine started, waiting for initialization...")
		// Wait a bit for the app to fully initialize
		time.Sleep(constants.AutoConnectDelay)
		logger.Log.Info().Msg("Starting auto-connect process...")

		networks, err := a.storage.GetNetworks()
		if err != nil {
			logger.Log.Error().Err(err).Msg("Failed to load networks for auto-connect")
			return
		}

		logger.Log.Info().Int("count", len(networks)).Msg("Found networks, checking for auto-connect...")

		// Log all networks and their auto-connect status
		for _, n := range networks {
			logger.Log.Debug().Str("network", n.Name).Int64("id", n.ID).Bool("auto_connect", n.AutoConnect).Msg("Network status")
		}

		// Count networks with auto-connect enabled for proper delay calculation
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

		// Track index of auto-connect networks for staggered delays
		autoConnectIndex := 0
		for _, network := range networks {
			if network.AutoConnect {
				// Capture network values for the goroutine
				networkID := network.ID
				networkName := network.Name
				delay := time.Duration(autoConnectIndex) * constants.ConnectionStaggerDelay
				autoConnectIndex++

				logger.Log.Info().Str("network", networkName).Int64("id", networkID).Dur("delay", delay).Msg("Scheduling auto-connect for network")

				// Connect in background with staggered delays to prevent race conditions
				a.startupWg.Add(1)
				go func(nID int64, nName string, d time.Duration) {
					defer a.startupWg.Done()
					defer func() {
						if r := recover(); r != nil {
							logger.Log.Error().Interface("panic", r).Str("network", nName).Msg("PANIC in auto-connect")
						}
					}()

					// Check if context is cancelled
					select {
					case <-a.startupCtx.Done():
						logger.Log.Debug().Str("network", nName).Msg("Startup cancelled, skipping auto-connect")
						return
					case <-time.After(d):
						// Continue with connection
					}
					logger.Log.Info().Str("network", nName).Int64("id", nID).Msg("Starting auto-connect for network")

					// Load servers for this network
					dbServers, err := a.storage.GetServers(nID)
					if err != nil || len(dbServers) == 0 {
						logger.Log.Error().Err(err).Str("network", nName).Msg("Failed to load servers")
						return
					}

					// Get network again to ensure we have latest data
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

					// Double-check that auto-connect is still enabled (user might have disabled it)
					if !network.AutoConnect {
						logger.Log.Info().Str("network", nName).Int64("id", nID).Msg("Network no longer has auto-connect enabled, skipping")
						return
					}

					// Build config from network and servers
					config := NetworkConfig{}
					// Convert servers
					serverConfigs := make([]ServerConfig, len(dbServers))
					for j, srv := range dbServers {
						serverConfigs[j] = ServerConfig{
							Address: srv.Address,
							Port:    srv.Port,
							TLS:     srv.TLS,
							Order:   srv.Order,
						}
					}
					config.Servers = serverConfigs
					config.Name = network.Name
					config.Nickname = network.Nickname
					config.Username = network.Username
					config.Realname = network.Realname
					config.Password = network.Password
					config.SASLEnabled = network.SASLEnabled
					config.AutoConnect = network.AutoConnect // Preserve auto-connect setting
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
					logger.Log.Debug().
						Str("nickname", config.Nickname).
						Str("username", config.Username).
						Str("realname", config.Realname).
						Bool("sasl_enabled", config.SASLEnabled).
						Msg("Config details")

					if err := a.ConnectNetwork(config); err != nil {
						logger.Log.Error().Err(err).Str("network", nName).Str("error_type", fmt.Sprintf("%T", err)).Msg("Failed to auto-connect")
					} else {
						logger.Log.Info().Str("network", nName).Msg("Auto-connected successfully")
						// Verify connection status after a short delay
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
	}()
}

// shutdown is called when the app shuts down
func (a *App) shutdown(ctx context.Context) {
	logger.Log.Info().Msg("App shutdown initiated")

	// Create a timeout context for shutdown operations
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Cancel startup context to stop any pending auto-connects
	if a.startupCancel != nil {
		a.startupCancel()
	}

	// Wait for startup goroutines to finish (with timeout)
	startupDone := make(chan struct{})
	go func() {
		a.startupWg.Wait()
		close(startupDone)
	}()
	select {
	case <-startupDone:
		// Startup goroutines finished
	case <-shutdownCtx.Done():
		logger.Log.Warn().Msg("Timeout waiting for startup goroutines, continuing shutdown")
	}

	// Send "Disconnected" message to each connected network's status window
	a.mu.Lock()
	for _, client := range a.ircClients {
		if client.IsConnected() {
			// Write disconnected message to status window
			client.WriteStatusMessage("Disconnected from server")
		}
		client.Disconnect()
	}
	a.mu.Unlock()

	// Close plugin manager (with timeout)
	if a.pluginManager != nil {
		pluginDone := make(chan struct{})
		go func() {
			a.pluginManager.Close()
			close(pluginDone)
		}()
		select {
		case <-pluginDone:
			// Plugin manager closed
		case <-shutdownCtx.Done():
			logger.Log.Warn().Msg("Timeout closing plugin manager, continuing shutdown")
		}
	}

	// Close storage (with timeout)
	if a.storage != nil {
		storageDone := make(chan struct{})
		go func() {
			a.storage.Close()
			close(storageDone)
		}()
		select {
		case <-storageDone:
			// Storage closed
		case <-shutdownCtx.Done():
			logger.Log.Warn().Msg("Timeout closing storage, continuing shutdown")
		}
	}

	logger.Log.Info().Msg("App shutdown complete")
}

// OnEvent implements the events.Subscriber interface to forward events to frontend
func (a *App) OnEvent(event events.Event) {
	// Ensure context is set before event emission
	if a.ctx == nil {
		logger.Log.Debug().Msg("OnEvent: context not yet initialized, skipping event")
		return
	}

	// Handle connection status events separately to include network ID
	if event.Type == irc.EventConnectionEstablished || event.Type == irc.EventConnectionLost {
		// Extract network address from event data
		networkAddress, ok := event.Data["network"].(string)
		if !ok {
			return
		}

		// Find the network ID by checking all networks and their servers
		var networkID int64
		found := false

		networks, err := a.storage.GetNetworks()
		if err == nil {
			for _, n := range networks {
				// Check primary address first (for backward compatibility)
				if n.Address == networkAddress {
					networkID = n.ID
					found = true
					break
				}

				// Check all servers for this network
				servers, err := a.storage.GetServers(n.ID)
				if err == nil {
					for _, srv := range servers {
						if srv.Address == networkAddress {
							networkID = n.ID
							found = true
							break
						}
					}
				}
				if found {
					break
				}
			}
		}

		if found {
			// Forward connection status event with network ID
			isConnected := event.Type == irc.EventConnectionEstablished
			runtime.EventsEmit(a.ctx, "connection-status", map[string]interface{}{
				"networkId": networkID,
				"connected": isConnected,
				"timestamp": event.Timestamp.Format(time.RFC3339),
			})
		}
		return
	}

	// Handle channels changed event separately for immediate sidebar updates
	if event.Type == irc.EventChannelsChanged {
		// Extract network ID from event data
		networkID, ok := event.Data["networkId"].(int64)
		if !ok {
			// Fallback: try to find network by address
			networkAddress, ok := event.Data["network"].(string)
			if ok {
				networks, err := a.storage.GetNetworks()
				if err == nil {
					for _, n := range networks {
						if n.Address == networkAddress {
							networkID = n.ID
							break
						}
					}
				}
			}
		}
		
		// Forward channels changed event to frontend
		runtime.EventsEmit(a.ctx, "channels-changed", map[string]interface{}{
			"networkId": networkID,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
		return
	}

	// Forward message and user events to frontend for real-time updates
	if event.Type == irc.EventMessageSent || event.Type == irc.EventMessageReceived ||
		event.Type == irc.EventUserJoined || event.Type == irc.EventUserParted || event.Type == irc.EventUserQuit ||
		event.Type == irc.EventUserKicked ||
		event.Type == irc.EventChannelTopic || event.Type == irc.EventChannelMode ||
		event.Type == irc.EventError || event.Type == "channel.names.complete" {
		// Forward to frontend via Wails events
		// Convert timestamp to ISO string for JSON serialization
		runtime.EventsEmit(a.ctx, "message-event", map[string]interface{}{
			"type":      event.Type,
			"data":      event.Data,
			"timestamp": event.Timestamp.Format(time.RFC3339),
		})
	}
}

// processPluginActions processes actions from plugins
func (a *App) processPluginActions() {
	for action := range a.pluginManager.GetActionQueue() {
		switch action.Type {
		case "send_message":
			// Extract data
			serverAddr, _ := action.Data["server"].(string) // server address
			target, _ := action.Data["target"].(string)
			message, _ := action.Data["message"].(string)

			// Find IRC client for server by matching network address
			a.mu.RLock()
			var client *irc.IRCClient
			if serverAddr != "" {
				// Try to find client by server address
				networks, err := a.storage.GetNetworks()
				if err == nil {
					for _, network := range networks {
						// Check primary address
						if network.Address == serverAddr {
							if c, exists := a.ircClients[network.ID]; exists && c != nil {
								client = c
								break
							}
						}
						// Check all servers for this network
						servers, err := a.storage.GetServers(network.ID)
						if err == nil {
							for _, srv := range servers {
								if srv.Address == serverAddr {
									if c, exists := a.ircClients[network.ID]; exists && c != nil {
										client = c
										break
									}
								}
							}
						}
						if client != nil {
							break
						}
					}
				}
			} else {
				// No server specified, use first available client
				for _, c := range a.ircClients {
					if c != nil {
						client = c
						break
					}
				}
			}
			a.mu.RUnlock()

			if client != nil {
				if err := client.SendMessage(target, message); err != nil {
					logger.Log.Error().Err(err).Str("target", target).Msg("Failed to send message from plugin")
				}
			} else {
				logger.Log.Warn().Str("server", serverAddr).Msg("No IRC client found for plugin action")
			}
		}
	}
}

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

// SaveNetwork saves network configuration without connecting
func (a *App) SaveNetwork(config NetworkConfig) error {
	// Normalize servers (support both old and new format)
	servers := a.normalizeServers(config)
	if len(servers) == 0 {
		return fmt.Errorf("no servers provided")
	}

	var network *storage.Network
	var err error

	// Check if network already exists (by name)
	networks, err := a.storage.GetNetworks()
	if err == nil {
		for _, n := range networks {
			if n.Name == config.Name {
				// Network exists, use it
				network = &n
				break
			}
		}
	}

	if network == nil {
		// Create new network record (use first server for legacy fields)
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
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}

		if err := a.storage.CreateNetwork(network); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}

		// Save all servers
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
				return fmt.Errorf("failed to create server: %w", err)
			}
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
		network.AutoConnect = config.AutoConnect
		network.UpdatedAt = time.Now()
		if err := a.storage.UpdateNetwork(network); err != nil {
			return fmt.Errorf("failed to update network: %w", err)
		}

		// Update servers: delete old ones and create new ones
		if err := a.storage.DeleteAllServers(network.ID); err != nil {
			return fmt.Errorf("failed to delete old servers: %w", err)
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
				return fmt.Errorf("failed to create server: %w", err)
			}
		}
	}

	return nil
}

// ConnectNetwork connects to an IRC network
// If networkID is provided (> 0), reconnects to existing network
// Otherwise, creates a new network
func (a *App) ConnectNetwork(config NetworkConfig) error {
	// Normalize servers (support both old and new format)
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

	var network *storage.Network
	var err error

	// Check if network already exists (by name)
	networks, err := a.storage.GetNetworks()
	if err == nil {
		for _, n := range networks {
			if n.Name == config.Name {
				// Network exists, use it
				network = &n
				break
			}
		}
	}

	if network == nil {
		// Create new network record (use first server for legacy fields)
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
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}

		if err := a.storage.CreateNetwork(network); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}

		// Save all servers
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
				return fmt.Errorf("failed to create server: %w", err)
			}
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
		network.AutoConnect = config.AutoConnect
		network.UpdatedAt = time.Now()
		if err := a.storage.UpdateNetwork(network); err != nil {
			return fmt.Errorf("failed to update network: %w", err)
		}

		// Update servers: delete old ones and create new ones
		if err := a.storage.DeleteAllServers(network.ID); err != nil {
			return fmt.Errorf("failed to delete old servers: %w", err)
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
				return fmt.Errorf("failed to create server: %w", err)
			}
		}
	}

	// Get servers from database (ordered by priority)
	dbServers, err := a.storage.GetServers(network.ID)
	if err != nil || len(dbServers) == 0 {
		// Fallback to servers from config
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

	// Create a unique key for this network connection (by network ID)
	networkKey := fmt.Sprintf("network:%d", network.ID)

	logger.Log.Info().Str("network", config.Name).Int("servers", len(dbServers)).Msg("Attempting to connect")

	// Lock for the entire connection process to prevent concurrent connections
	a.mu.Lock()

	// Check if a connection is already in progress for this network
	if _, inProgress := a.connectingNetworks[networkKey]; inProgress {
		logger.Log.Warn().Str("network_key", networkKey).Msg("Connection already in progress, rejecting")
		a.mu.Unlock()
		return fmt.Errorf("connection to %s already in progress", config.Name)
	}

	// Check if already connected to this network (by ID)
	if existingClient, exists := a.ircClients[network.ID]; exists {
		if existingClient.IsConnected() {
			logger.Log.Warn().Str("network", config.Name).Int64("id", network.ID).Msg("Already connected, rejecting")
			a.mu.Unlock()
			return fmt.Errorf("already connected to %s", config.Name)
		}
	}

	// Mark that we're connecting to this network
	connectDone := make(chan struct{})
	a.connectingNetworks[networkKey] = connectDone
	logger.Log.Debug().Str("network_key", networkKey).Msg("Marked as connecting")

	// Disconnect if already connected to this network (by ID)
	if existingClient, exists := a.ircClients[network.ID]; exists {
		if existingClient.IsConnected() {
			existingClient.Disconnect()
		}
		delete(a.ircClients, network.ID)
	}

	a.mu.Unlock()

	// Wait a bit for any existing connection to clean up
	time.Sleep(constants.ConnectionCleanupDelay)

	// Try connecting to servers in order
	var lastErr error
	for i, srv := range dbServers {
		serverKey := fmt.Sprintf("%s:%d", srv.Address, srv.Port)
		logger.Log.Info().Int("current", i+1).Int("total", len(dbServers)).Str("server", serverKey).Msg("Trying server")

		// Write status message
		statusMsg := storage.Message{
			NetworkID:   network.ID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Connecting to %s:%d...", srv.Address, srv.Port),
			MessageType: "status",
			Timestamp:   time.Now(),
			RawLine:     "",
		}
		a.storage.WriteMessage(statusMsg)

		// Create a temporary network object with this server's address
		tempNetwork := *network
		tempNetwork.Address = srv.Address
		tempNetwork.Port = srv.Port
		tempNetwork.TLS = srv.TLS

		// Debug: Log network configuration
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

		// Create IRC client with this server address
		ircClient := irc.NewIRCClient(&tempNetwork, a.eventBus, a.storage)
		ircClient.SetNetworkID(network.ID)

		// Try to connect with timeout
		logger.Log.Debug().Str("server", serverKey).Msg("Starting connection attempt")
		connectErr := make(chan error, 1)
		go func() {
			connectErr <- ircClient.Connect()
		}()

		var err error
		select {
		case err = <-connectErr:
			// Connection attempt completed (success or failure)
			if err != nil {
				logger.Log.Debug().Err(err).Str("server", serverKey).Msg("Connection attempt failed")
			} else {
				logger.Log.Debug().Str("server", serverKey).Msg("Connection attempt succeeded")
			}
		case <-time.After(30 * time.Second):
			// Connection timeout - disconnect and try next server
			logger.Log.Warn().Str("server", serverKey).Msg("Connection timeout after 30 seconds")
			ircClient.Disconnect()
			err = fmt.Errorf("connection timeout after 30 seconds")
		}

		if err != nil {
			lastErr = err
			logger.Log.Warn().Err(err).Str("server", serverKey).Msg("Failed to connect")

			// Write error status message
			errorMsg := storage.Message{
				NetworkID:   network.ID,
				ChannelID:   nil,
				User:        "*",
				Message:     fmt.Sprintf("Failed to connect to %s:%d: %v", srv.Address, srv.Port, err),
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     "",
			}
			a.storage.WriteMessage(errorMsg)

			// Try next server
			continue
		}

		// Success! Store client and clear connection-in-progress flag
		logger.Log.Info().Str("server", serverKey).Int64("network_id", network.ID).Msg("Connection successful")
		a.mu.Lock()
		a.ircClients[network.ID] = ircClient
		delete(a.connectingNetworks, networkKey)
		close(connectDone)
		a.mu.Unlock()

		// Write success message
		successMsg := storage.Message{
			NetworkID:   network.ID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Connected to %s:%d", srv.Address, srv.Port),
			MessageType: "status",
			Timestamp:   time.Now(),
			RawLine:     "",
		}
		a.storage.WriteMessage(successMsg)

		return nil
	}

	// All servers failed
	logger.Log.Error().Str("network", config.Name).Msg("All connection attempts failed")
	a.mu.Lock()
	delete(a.connectingNetworks, networkKey)
	close(connectDone)
	a.mu.Unlock()

	// Write final error message
	errorMsg := storage.Message{
		NetworkID:   network.ID,
		ChannelID:   nil,
		User:        "*",
		Message:     fmt.Sprintf("Failed to connect to any server: %v", lastErr),
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	a.storage.WriteMessage(errorMsg)

	return fmt.Errorf("failed to connect to any server: %w", lastErr)
}

// SendMessage sends a message to a channel or user
func (a *App) SendMessage(networkID int64, target, message string) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}

	return client.SendMessage(target, message)
}

// SendCommand sends a command from any channel or status window
// Supports commands like /join #channel, /msg user message, or raw IRC commands
func (a *App) SendCommand(networkID int64, command string) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}

	// Parse command
	command = strings.TrimSpace(command)
	if len(command) == 0 {
		return fmt.Errorf("empty command")
	}

	// Handle slash commands
	if strings.HasPrefix(command, "/") {
		parts := strings.Fields(command[1:]) // Remove leading /
		if len(parts) == 0 {
			return fmt.Errorf("invalid command")
		}

		cmd := strings.ToUpper(parts[0])
		switch cmd {
		case "JOIN", "J":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /join #channel [key]")
			}
			channelName := parts[1]
			// If channel has a key, we need to send raw JOIN command
			if len(parts) >= 3 {
				rawCmd := fmt.Sprintf("JOIN %s %s", channelName, parts[2])
				return client.SendRawCommand(rawCmd)
			}
			logger.Log.Info().Str("channel", channelName).Msg("Joining channel")
			if err := client.JoinChannel(channelName); err != nil {
				logger.Log.Error().Err(err).Str("channel", channelName).Msg("Error joining channel")
				return err
			}
			logger.Log.Info().Str("channel", channelName).Msg("Successfully sent JOIN command")
			return nil
		case "PART", "LEAVE":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /part #channel [reason]")
			}
			channelName := parts[1]
			var rawCmd string
			if len(parts) >= 3 {
				reason := strings.Join(parts[2:], " ")
				rawCmd = fmt.Sprintf("PART %s :%s", channelName, reason)
			} else {
				rawCmd = fmt.Sprintf("PART %s", channelName)
			}
			// Use PartChannel for basic part, or send raw if there's a reason
			if len(parts) >= 3 {
				return client.SendRawCommand(rawCmd)
			}
			return client.PartChannel(channelName)
		case "MSG", "PRIVMSG", "M":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /msg target message")
			}
			target := parts[1]
			message := strings.Join(parts[2:], " ")
			return client.SendMessage(target, message)
		case "NICK":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /nick newnick")
			}
			return client.SendRawCommand(fmt.Sprintf("NICK %s", parts[1]))
		case "QUIT":
			reason := ""
			if len(parts) >= 2 {
				reason = strings.Join(parts[1:], " ")
			}
			if reason != "" {
				return client.SendRawCommand(fmt.Sprintf("QUIT :%s", reason))
			}
			return client.SendRawCommand("QUIT")
		case "AWAY":
			message := ""
			if len(parts) >= 2 {
				message = strings.Join(parts[1:], " ")
			}
			if message != "" {
				return client.SendRawCommand(fmt.Sprintf("AWAY :%s", message))
			}
			// Empty away message means unset away
			return client.SendRawCommand("AWAY")
		case "WHOIS":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /whois nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("WHOIS %s", parts[1]))
		case "WHOWAS":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /whowas nickname")
			}
			return client.SendRawCommand(fmt.Sprintf("WHOWAS %s", parts[1]))
		case "ME", "ACTION":
			// /me action text - sends CTCP ACTION
			// Format: /me [channel] action text
			// If channel is specified as first arg (starts with # or &), use it
			// Otherwise, channel should be encoded in command from frontend
			if len(parts) < 2 {
				return fmt.Errorf("usage: /me action text")
			}
			var target string
			var actionText string
			// Check if first argument is a channel
			if len(parts) >= 2 && (strings.HasPrefix(parts[1], "#") || strings.HasPrefix(parts[1], "&")) {
				// Channel specified as first argument
				target = parts[1]
				if len(parts) >= 3 {
					actionText = strings.Join(parts[2:], " ")
				} else {
					return fmt.Errorf("usage: /me [channel] action text")
				}
			} else {
				// No channel specified - this should have been handled by frontend
				// But we'll return an error to be safe
				return fmt.Errorf("usage: /me action text (must be used in a channel)")
			}
			// Send CTCP ACTION: PRIVMSG target :\001ACTION text\001
			return client.SendRawCommand(fmt.Sprintf("PRIVMSG %s :\001ACTION %s\001", target, actionText))
		case "TOPIC":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /topic #channel [new topic]")
			}
			channelName := parts[1]
			if len(parts) >= 3 {
				topic := strings.Join(parts[2:], " ")
				return client.SendRawCommand(fmt.Sprintf("TOPIC %s :%s", channelName, topic))
			}
			// Just query topic
			return client.SendRawCommand(fmt.Sprintf("TOPIC %s", channelName))
		case "MODE":
			if len(parts) < 2 {
				return fmt.Errorf("usage: /mode target modes [args]")
			}
			target := parts[1]
			var rawCmd string
			if len(parts) >= 3 {
				// Mode change
				modeArgs := strings.Join(parts[2:], " ")
				rawCmd = fmt.Sprintf("MODE %s %s", target, modeArgs)
			} else {
				// Query mode
				rawCmd = fmt.Sprintf("MODE %s", target)
			}
			return client.SendRawCommand(rawCmd)
		case "INVITE":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /invite nickname #channel")
			}
			nickname := parts[1]
			channelName := parts[2]
			return client.SendRawCommand(fmt.Sprintf("INVITE %s %s", nickname, channelName))
		case "KICK":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /kick #channel nickname [reason]")
			}
			channelName := parts[1]
			nickname := parts[2]
			var rawCmd string
			if len(parts) >= 4 {
				reason := strings.Join(parts[3:], " ")
				rawCmd = fmt.Sprintf("KICK %s %s :%s", channelName, nickname, reason)
			} else {
				rawCmd = fmt.Sprintf("KICK %s %s", channelName, nickname)
			}
			return client.SendRawCommand(rawCmd)
		case "BAN":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /ban #channel mask")
			}
			channelName := parts[1]
			mask := parts[2]
			return client.SendRawCommand(fmt.Sprintf("MODE %s +b %s", channelName, mask))
		case "UNBAN":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /unban #channel mask")
			}
			channelName := parts[1]
			mask := parts[2]
			return client.SendRawCommand(fmt.Sprintf("MODE %s -b %s", channelName, mask))
		case "OP", "HOP":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /op #channel nickname")
			}
			channelName := parts[1]
			nickname := parts[2]
			return client.SendRawCommand(fmt.Sprintf("MODE %s +o %s", channelName, nickname))
		case "DEOP", "DEHOP":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /deop #channel nickname")
			}
			channelName := parts[1]
			nickname := parts[2]
			return client.SendRawCommand(fmt.Sprintf("MODE %s -o %s", channelName, nickname))
		case "VOICE", "V":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /voice #channel nickname")
			}
			channelName := parts[1]
			nickname := parts[2]
			return client.SendRawCommand(fmt.Sprintf("MODE %s +v %s", channelName, nickname))
		case "DEVOICE", "DEV":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /devoice #channel nickname")
			}
			channelName := parts[1]
			nickname := parts[2]
			return client.SendRawCommand(fmt.Sprintf("MODE %s -v %s", channelName, nickname))
		case "LIST":
			var rawCmd string
			if len(parts) >= 2 {
				// List specific channels or with filters
				args := strings.Join(parts[1:], " ")
				rawCmd = fmt.Sprintf("LIST %s", args)
			} else {
				rawCmd = "LIST"
			}
			return client.SendRawCommand(rawCmd)
		case "NAMES":
			var rawCmd string
			if len(parts) >= 2 {
				channelName := parts[1]
				rawCmd = fmt.Sprintf("NAMES %s", channelName)
			} else {
				rawCmd = "NAMES"
			}
			return client.SendRawCommand(rawCmd)
		case "NOTICE":
			if len(parts) < 3 {
				return fmt.Errorf("usage: /notice target message")
			}
			target := parts[1]
			message := strings.Join(parts[2:], " ")
			return client.SendRawCommand(fmt.Sprintf("NOTICE %s :%s", target, message))
		case "QUERY", "Q":
			// Query opens a private message window - this is client-side only
			// We'll just send a message to the user
			if len(parts) < 2 {
				return fmt.Errorf("usage: /query nickname [message]")
			}
			nickname := parts[1]
			if len(parts) >= 3 {
				message := strings.Join(parts[2:], " ")
				return client.SendMessage(nickname, message)
			}
			// Just opening query window - client should handle this
			// For now, we'll just return success (client should switch to PM window)
			return nil
		case "CLOSE":
			// Close a channel/query window - this is client-side only
			// We'll part if it's a channel
			if len(parts) < 2 {
				return fmt.Errorf("usage: /close #channel or /close nickname")
			}
			target := parts[1]
			// If it's a channel, part it
			if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
				return client.PartChannel(target)
			}
			// For PM windows, this is client-side only
			return nil
		case "QUOTE", "RAW":
			// Send raw IRC command
			if len(parts) < 2 {
				return fmt.Errorf("usage: /quote command [args] or /raw command [args]")
			}
			rawCommand := strings.Join(parts[1:], " ")
			return client.SendRawCommand(rawCommand)
		case "IGNORE":
			// Client-side ignore - this would need to be implemented client-side
			if len(parts) < 2 {
				return fmt.Errorf("usage: /ignore nickname")
			}
			// This is a client-side feature, not an IRC command
			return fmt.Errorf("/ignore is not yet implemented")
		case "UNIGNORE":
			// Client-side unignore
			if len(parts) < 2 {
				return fmt.Errorf("usage: /unignore nickname")
			}
			return fmt.Errorf("/unignore is not yet implemented")
		default:
			// Unknown command, send as raw IRC command
			return client.SendRawCommand(command[1:]) // Remove leading /
		}
	}

	// Not a slash command, send as raw IRC command
	return client.SendRawCommand(command)
}

// GetMessages retrieves messages for a network and channel
// If channelID is nil, returns status window messages
func (a *App) GetMessages(networkID int64, channelID *int64, limit int) ([]storage.Message, error) {
	return a.storage.GetMessages(networkID, channelID, limit)
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

	// Remove from clients map
	delete(a.ircClients, networkID)

	return nil
}

// DeleteNetwork deletes a network configuration
func (a *App) DeleteNetwork(networkID int64) error {
	// Disconnect if connected
	a.mu.Lock()
	client, exists := a.ircClients[networkID]
	if exists {
		client.Disconnect()
		delete(a.ircClients, networkID)
	}
	a.mu.Unlock()

	// Delete from database
	return a.storage.DeleteNetwork(networkID)
}

// GetNetworks retrieves all networks
func (a *App) GetNetworks() ([]storage.Network, error) {
	return a.storage.GetNetworks()
}

// GetChannels retrieves channels for a network
func (a *App) GetChannels(networkID int64) ([]storage.Channel, error) {
	return a.storage.GetChannels(networkID)
}

// GetJoinedChannels retrieves channels for a network where the current user is still a member
func (a *App) GetJoinedChannels(networkID int64) ([]storage.Channel, error) {
	// Get network to find the current nickname
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}
	
	if network.Nickname == "" {
		// No nickname set, return empty list
		return []storage.Channel{}, nil
	}
	
	return a.storage.GetJoinedChannels(networkID, network.Nickname)
}

// GetOpenChannels retrieves channels for a network where the dialog is open or where the user is joined
func (a *App) GetOpenChannels(networkID int64) ([]storage.Channel, error) {
	// Get network to find the current nickname
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}
	
	if network.Nickname == "" {
		// No nickname set, return channels that are open
		return a.storage.GetChannels(networkID)
	}
	
	return a.storage.GetOpenChannels(networkID, network.Nickname)
}

// SetChannelOpen sets the is_open state for a channel
func (a *App) SetChannelOpen(networkID int64, channelName string, isOpen bool) error {
	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}
	
	return a.storage.UpdateChannelIsOpen(channel.ID, isOpen)
}

// GetChannelIDByName retrieves a channel ID by network ID and channel name
func (a *App) GetChannelIDByName(networkID int64, channelName string) (*int64, error) {
	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		return nil, err
	}
	return &channel.ID, nil
}

// ChannelInfo represents channel information with users
type ChannelInfo struct {
	Channel    *storage.Channel       `json:"channel"`
	Users      []storage.ChannelUser  `json:"users"`
	Capabilities *ServerCapabilitiesInfo `json:"capabilities,omitempty"`
}

// ServerCapabilitiesInfo represents server capabilities for frontend
type ServerCapabilitiesInfo struct {
	Prefix      map[string]string `json:"prefix"`       // Map prefix char to mode char (e.g., "@" -> "o", "+" -> "v")
	PrefixString string           `json:"prefix_string"` // Raw PREFIX string
	ChanModes   string           `json:"chanmodes"`     // Raw CHANMODES string
}

// GetChannelInfo retrieves channel information including topic and users
func (a *App) GetChannelInfo(networkID int64, channelName string) (*ChannelInfo, error) {
	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		return nil, err
	}

	// Only return users if the network is actually connected
	// This prevents showing stale user lists when disconnected
	var users []storage.ChannelUser
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	isConnected := exists && client.IsConnected()
	a.mu.RUnlock()

	if isConnected {
		users, err = a.storage.GetChannelUsers(channel.ID)
		if err != nil {
			users = []storage.ChannelUser{}
		}
	} else {
		// Network is not connected, return empty user list
		users = []storage.ChannelUser{}
	}

	// Get server capabilities
	capabilities, _ := a.GetServerCapabilities(networkID)

	return &ChannelInfo{
		Channel:     channel,
		Users:       users,
		Capabilities: capabilities,
	}, nil
}

// GetServerCapabilities retrieves server capabilities from ISUPPORT for a network
func (a *App) GetServerCapabilities(networkID int64) (*ServerCapabilitiesInfo, error) {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return &ServerCapabilitiesInfo{
			Prefix:      make(map[string]string),
			PrefixString: "",
			ChanModes:   "",
		}, nil
	}

	cap := client.GetServerCapabilities()

	// Convert rune map to string map for JSON serialization
	prefixMap := make(map[string]string)
	for k, v := range cap.Prefix {
		prefixMap[string(k)] = string(v)
	}

	return &ServerCapabilitiesInfo{
		Prefix:      prefixMap,
		PrefixString: cap.PrefixString,
		ChanModes:   cap.ChanModes,
	}, nil
}

// ToggleChannelAutoJoin toggles the auto-join setting for a channel
func (a *App) ToggleChannelAutoJoin(networkID int64, channelName string) error {
	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	newAutoJoin := !channel.AutoJoin
	logger.Log.Info().
		Str("channel", channelName).
		Int64("network_id", networkID).
		Bool("old_value", channel.AutoJoin).
		Bool("new_value", newAutoJoin).
		Msg("Toggling auto-join for channel")

	if err := a.storage.UpdateChannelAutoJoin(channel.ID, newAutoJoin); err != nil {
		return fmt.Errorf("failed to update auto-join: %w", err)
	}

	return nil
}

// LeaveChannel leaves an IRC channel
func (a *App) LeaveChannel(networkID int64, channelName string) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}

	if !client.IsConnected() {
		return fmt.Errorf("network not connected")
	}

	logger.Log.Info().Str("channel", channelName).Int64("network_id", networkID).Msg("Leaving channel")
	if err := client.PartChannel(channelName); err != nil {
		return fmt.Errorf("failed to leave channel: %w", err)
	}

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
		return fmt.Errorf("failed to update auto-connect: %w", err)
	}

	return nil
}

// GetServers retrieves server addresses for a network
func (a *App) GetServers(networkID int64) ([]storage.Server, error) {
	return a.storage.GetServers(networkID)
}

// ListPlugins returns information about all plugins
func (a *App) ListPlugins() []*plugin.PluginInfo {
	return a.pluginManager.ListPlugins()
}

// EnablePlugin enables a plugin
func (a *App) EnablePlugin(name string) error {
	// Would need to implement plugin enable/disable logic
	return fmt.Errorf("not implemented")
}

// DisablePlugin disables a plugin
func (a *App) DisablePlugin(name string) error {
	return a.pluginManager.UnloadPlugin(name)
}

// Greet returns a greeting for the given name (kept for compatibility)
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// OpenSettings emits an event to open the settings modal
func (a *App) OpenSettings() {
	logger.Log.Debug().Msg("OpenSettings called")
	if a.ctx != nil {
		logger.Log.Debug().Msg("Emitting open-settings event")
		runtime.EventsEmit(a.ctx, "open-settings")
	} else {
		logger.Log.Warn().Msg("OpenSettings: context is nil")
	}
}
