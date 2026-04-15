package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/notification"
	"github.com/matt0x6f/irc-client/internal/plugin"
	"github.com/matt0x6f/irc-client/internal/security"
	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx                  context.Context
	storage              *storage.Storage
	eventBus             *events.EventBus
	pluginManager        *plugin.Manager
	keychain             *security.Keychain
	notifier             *notification.Notifier
	ircClients           map[int64]*irc.IRCClient
	connectingNetworks   map[string]chan struct{} // Track networks currently connecting by "address:port"
	reconnectingNetworks map[int64]bool           // Track networks currently reconnecting (by network ID)
	mu                   sync.RWMutex
	startupCtx           context.Context
	startupCancel        context.CancelFunc
	startupWg            sync.WaitGroup
	shutdownOnce         sync.Once // Ensure shutdown only runs once
}

// NewApp creates a new App application struct
func NewApp() (*App, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	dbPath := filepath.Join(homeDir, ".cascade-chat", "cascade-chat.db")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	stor, err := storage.NewStorage(dbPath, 100, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	eventBus := events.NewEventBus()
	keychain := security.NewKeychain()

	pluginDir := filepath.Join(homeDir, ".cascade-chat", "plugins")
	pluginMgr := plugin.NewManager(eventBus, pluginDir)
	pluginMgr.SetStorage(stor)

	notifier := notification.NewNotifier()

	app := &App{
		storage:              stor,
		eventBus:             eventBus,
		pluginManager:        pluginMgr,
		keychain:             keychain,
		notifier:             notifier,
		ircClients:           make(map[int64]*irc.IRCClient),
		connectingNetworks:   make(map[string]chan struct{}),
		reconnectingNetworks: make(map[int64]bool),
	}

	// Subscribe to events for frontend forwarding
	eventBus.Subscribe(irc.EventMessageSent, app)
	eventBus.Subscribe(irc.EventMessageReceived, app)
	eventBus.Subscribe(irc.EventConnectionEstablished, app)
	eventBus.Subscribe(irc.EventConnectionLost, app)
	eventBus.Subscribe(irc.EventChannelsChanged, app)
	eventBus.Subscribe(irc.EventUserJoined, app)
	eventBus.Subscribe(irc.EventUserParted, app)
	eventBus.Subscribe(irc.EventUserQuit, app)
	eventBus.Subscribe(irc.EventUserKicked, app)
	eventBus.Subscribe(irc.EventUserNick, app)
	eventBus.Subscribe(irc.EventWhoisReceived, app)
	eventBus.Subscribe(irc.EventChannelListEnd, app)

	go app.processPluginActions()

	return app, nil
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startupCtx, a.startupCancel = context.WithCancel(context.Background())
	logger.Log.Info().Msg("App startup function called")

	// Track window focus state for notification suppression.
	// The frontend emits these events via window focus/blur DOM listeners.
	runtime.EventsOn(ctx, "window-focused", func(optionalData ...interface{}) {
		a.notifier.SetFocused(true)
	})
	runtime.EventsOn(ctx, "window-blurred", func(optionalData ...interface{}) {
		a.notifier.SetFocused(false)
	})

	// Load plugins in background so it doesn't block auto-connect
	go func() {
		if err := a.pluginManager.DiscoverAndLoad(); err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to load plugins")
		} else {
			plugins := a.pluginManager.ListPlugins()
			logger.Log.Info().Int("count", len(plugins)).Msg("Plugins loaded successfully")
			for _, p := range plugins {
				logger.Log.Info().Str("name", p.Name).Str("path", p.Path).Msg("Loaded plugin")
			}
		}
	}()

	logger.Log.Info().Msg("Plugin loading started in background, proceeding to auto-connect setup")

	// Auto-connect networks
	a.startupWg.Add(1)
	go func() {
		defer a.startupWg.Done()
		a.autoConnect(a.startupCtx)
	}()
}

// shutdown is called when the app shuts down
func (a *App) shutdown(ctx context.Context) {
	a.shutdownOnce.Do(func() {
		logger.Log.Info().Msg("App shutdown initiated")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Cancel startup context to stop any pending auto-connects
		if a.startupCancel != nil {
			a.startupCancel()
		}

		// Wait for startup goroutines to finish
		logger.Log.Debug().Msg("Waiting for startup goroutines to finish")
		startupDone := make(chan struct{})
		go func() {
			a.startupWg.Wait()
			close(startupDone)
		}()
		select {
		case <-startupDone:
			logger.Log.Debug().Msg("Startup goroutines finished")
		case <-shutdownCtx.Done():
			logger.Log.Warn().Msg("Timeout waiting for startup goroutines, continuing shutdown")
		}

		// Write disconnect messages before closing anything
		logger.Log.Info().Msg("Starting disconnect message writing")
		a.writeShutdownDisconnectMessages()

		// Close plugin manager
		if a.pluginManager != nil {
			pluginDone := make(chan struct{})
			go func() {
				a.pluginManager.Close()
				close(pluginDone)
			}()
			select {
			case <-pluginDone:
			case <-shutdownCtx.Done():
				logger.Log.Warn().Msg("Timeout closing plugin manager, continuing shutdown")
			}
		}

		// Close storage
		if a.storage != nil {
			storageDone := make(chan struct{})
			go func() {
				a.storage.Close()
				close(storageDone)
			}()
			select {
			case <-storageDone:
			case <-shutdownCtx.Done():
				logger.Log.Warn().Msg("Timeout closing storage, continuing shutdown")
			}
		}

		logger.Log.Info().Msg("App shutdown complete")
	})
}

// --- Data retrieval methods (thin wrappers over storage) ---

// GetNetworks retrieves all networks
func (a *App) GetNetworks() ([]storage.Network, error) {
	return a.storage.GetNetworks()
}

// GetServers retrieves server addresses for a network
func (a *App) GetServers(networkID int64) ([]storage.Server, error) {
	return a.storage.GetServers(networkID)
}

// GetMessages retrieves messages for a network and channel
func (a *App) GetMessages(networkID int64, channelID *int64, limit int) ([]storage.Message, error) {
	return a.storage.GetMessages(networkID, channelID, limit)
}

// GetPrivateMessages retrieves private messages for a network and user
func (a *App) GetPrivateMessages(networkID int64, targetUser string, limit int) ([]storage.Message, error) {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}
	if network.Nickname == "" {
		return a.storage.GetPrivateMessages(networkID, targetUser, "", limit)
	}
	return a.storage.GetPrivateMessages(networkID, targetUser, network.Nickname, limit)
}

// GetPrivateMessageConversations retrieves a list of users with private message conversations
func (a *App) GetPrivateMessageConversations(networkID int64, openOnly bool) ([]string, error) {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}
	if network.Nickname == "" {
		return []string{}, nil
	}
	return a.storage.GetPrivateMessageConversations(networkID, network.Nickname, openOnly)
}

// GetChannels retrieves channels for a network
func (a *App) GetChannels(networkID int64) ([]storage.Channel, error) {
	return a.storage.GetChannels(networkID)
}

// GetJoinedChannels retrieves channels where the current user is still a member
func (a *App) GetJoinedChannels(networkID int64) ([]storage.Channel, error) {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}
	if network.Nickname == "" {
		return []storage.Channel{}, nil
	}
	return a.storage.GetJoinedChannels(networkID, network.Nickname)
}

// GetOpenChannels retrieves channels where the dialog is open or the user is joined
func (a *App) GetOpenChannels(networkID int64) ([]storage.Channel, error) {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}
	if network.Nickname == "" {
		return a.storage.GetChannels(networkID)
	}
	return a.storage.GetOpenChannels(networkID, network.Nickname)
}

// GetChannelIDByName retrieves a channel ID by network ID and channel name
func (a *App) GetChannelIDByName(networkID int64, channelName string) (*int64, error) {
	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		return nil, err
	}
	return &channel.ID, nil
}

// --- Channel & Pane management ---

// ChannelInfo represents channel information with users
type ChannelInfo struct {
	Channel      *storage.Channel       `json:"channel"`
	Users        []storage.ChannelUser  `json:"users"`
	Capabilities *ServerCapabilitiesInfo `json:"capabilities,omitempty"`
}

// ServerCapabilitiesInfo represents server capabilities for frontend
type ServerCapabilitiesInfo struct {
	Prefix       map[string]string `json:"prefix"`
	PrefixString string            `json:"prefix_string"`
	ChanModes    string            `json:"chanmodes"`
}

// GetChannelInfo retrieves channel information including topic and users
func (a *App) GetChannelInfo(networkID int64, channelName string) (*ChannelInfo, error) {
	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		return nil, err
	}

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
		users = []storage.ChannelUser{}
	}

	capabilities, _ := a.GetServerCapabilities(networkID)

	return &ChannelInfo{
		Channel:      channel,
		Users:        users,
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
			Prefix:       make(map[string]string),
			PrefixString: "",
			ChanModes:    "",
		}, nil
	}

	cap := client.GetServerCapabilities()
	prefixMap := make(map[string]string)
	for k, v := range cap.Prefix {
		prefixMap[string(k)] = string(v)
	}

	return &ServerCapabilitiesInfo{
		Prefix:       prefixMap,
		PrefixString: cap.PrefixString,
		ChanModes:    cap.ChanModes,
	}, nil
}

// SetChannelOpen sets the is_open state for a channel
func (a *App) SetChannelOpen(networkID int64, channelName string, isOpen bool) error {
	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		return fmt.Errorf("channel not found: %w", err)
	}

	err = a.storage.UpdateChannelIsOpen(channel.ID, isOpen)
	if err != nil {
		return err
	}

	if isOpen {
		// Request fresh NAMES list if user is joined to the channel
		a.mu.RLock()
		client, exists := a.ircClients[networkID]
		isConnected := exists && client.IsConnected()
		a.mu.RUnlock()

		if isConnected {
			network, err := a.storage.GetNetwork(networkID)
			if err == nil && network.Nickname != "" {
				joinedChannels, err := a.storage.GetJoinedChannels(networkID, network.Nickname)
				if err == nil {
					for _, joinedChannel := range joinedChannels {
						if strings.EqualFold(joinedChannel.Name, channelName) {
							logger.Log.Debug().Str("channel", channelName).Int64("network_id", networkID).Msg("Requesting fresh NAMES list for opened channel")
							if err := client.SendRawCommand(fmt.Sprintf("NAMES %s", channelName)); err != nil {
								logger.Log.Warn().Err(err).Str("channel", channelName).Msg("Failed to request NAMES list")
							}
							break
						}
					}
				}
			}
		}

		a.eventBus.Emit(events.Event{
			Type: events.EventUIPaneFocused,
			Data: map[string]interface{}{
				"networkId": networkID,
				"type":      "channel",
				"name":      channelName,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
	} else {
		a.eventBus.Emit(events.Event{
			Type: events.EventUIPaneBlurred,
			Data: map[string]interface{}{
				"networkId": networkID,
				"type":      "channel",
				"name":      channelName,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
	}

	return nil
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

	return a.storage.UpdateChannelAutoJoin(channel.ID, newAutoJoin)
}

// LeaveChannel leaves an IRC channel
func (a *App) LeaveChannel(networkID int64, channelName string) error {
	network, err := a.storage.GetNetwork(networkID)
	if err != nil {
		return fmt.Errorf("network not found: %w", err)
	}

	channel, err := a.storage.GetChannelByName(networkID, channelName)
	if err != nil {
		logger.Log.Debug().Str("channel", channelName).Int64("network_id", networkID).Msg("Channel not found in database, closing pane anyway")
		if err := a.SetChannelOpen(networkID, channelName, false); err != nil {
			logger.Log.Debug().Err(err).Msg("Channel not found, but emitting channels-changed event anyway")
		}
		a.eventBus.Emit(events.Event{
			Type: irc.EventChannelsChanged,
			Data: map[string]interface{}{
				"network":   network.Address,
				"networkId": networkID,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
		return nil
	}

	channelUsers, err := a.storage.GetChannelUsers(channel.ID)
	if err != nil {
		logger.Log.Warn().Err(err).Str("channel", channelName).Msg("Failed to get channel users, assuming not joined")
		if err := a.SetChannelOpen(networkID, channelName, false); err != nil {
			return err
		}
		a.eventBus.Emit(events.Event{
			Type: irc.EventChannelsChanged,
			Data: map[string]interface{}{
				"network":   network.Address,
				"networkId": networkID,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
		return nil
	}

	isJoined := false
	for _, user := range channelUsers {
		if strings.EqualFold(user.Nickname, network.Nickname) {
			isJoined = true
			break
		}
	}

	if !isJoined {
		logger.Log.Info().Str("channel", channelName).Int64("network_id", networkID).Msg("Not joined to channel, closing pane directly")
		if err := a.SetChannelOpen(networkID, channelName, false); err != nil {
			return err
		}
		a.eventBus.Emit(events.Event{
			Type: irc.EventChannelsChanged,
			Data: map[string]interface{}{
				"network":   network.Address,
				"networkId": networkID,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
		return nil
	}

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
	return client.PartChannel(channelName)
}

// --- Private message / pane focus management ---

// SetPrivateMessageOpen sets the is_open state for a private message conversation
func (a *App) SetPrivateMessageOpen(networkID int64, targetUser string, isOpen bool) error {
	originalCase := targetUser

	err := a.storage.UpdatePMConversationIsOpen(networkID, targetUser, isOpen)
	if err != nil {
		return err
	}

	// Try to get original case from messages
	network, err := a.storage.GetNetwork(networkID)
	if err == nil {
		messages, err := a.storage.GetPrivateMessages(networkID, targetUser, network.Nickname, 10)
		if err == nil && len(messages) > 0 {
			for _, msg := range messages {
				if !strings.EqualFold(msg.User, network.Nickname) {
					originalCase = msg.User
					break
				}
			}
		}
	}

	if isOpen {
		a.eventBus.Emit(events.Event{
			Type: events.EventUIPaneFocused,
			Data: map[string]interface{}{
				"networkId": networkID,
				"type":      "pm",
				"name":      originalCase,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
	} else {
		a.eventBus.Emit(events.Event{
			Type: events.EventUIPaneBlurred,
			Data: map[string]interface{}{
				"networkId": networkID,
				"type":      "pm",
				"name":      originalCase,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
	}

	return nil
}

// LastOpenPane represents the last open pane (channel or PM conversation)
type LastOpenPane struct {
	NetworkID int64  `json:"network_id"`
	Type      string `json:"type"` // "channel" or "pm"
	Name      string `json:"name"` // Channel name or PM target user
}

// GetLastOpenPane retrieves the most recently updated open channel or PM conversation
func (a *App) GetLastOpenPane() (*LastOpenPane, error) {
	pane, err := a.storage.GetLastOpenPane()
	if err != nil {
		return nil, err
	}
	if pane == nil {
		return nil, nil
	}
	return &LastOpenPane{
		NetworkID: pane.NetworkID,
		Type:      pane.Type,
		Name:      pane.Name,
	}, nil
}

// SetPaneFocus sets focus on a pane and emits an event
func (a *App) SetPaneFocus(networkID int64, paneType string, paneName string) error {
	switch paneType {
	case "channel":
		return a.SetChannelOpen(networkID, paneName, true)
	case "pm":
		return a.SetPrivateMessageOpen(networkID, paneName, true)
	case "status":
		a.eventBus.Emit(events.Event{
			Type: events.EventUIPaneFocused,
			Data: map[string]interface{}{
				"networkId": networkID,
				"type":      "status",
				"name":      "status",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
		return nil
	default:
		return fmt.Errorf("unknown pane type: %s", paneType)
	}
}

// ClearPaneFocus clears focus from a pane and emits an event
func (a *App) ClearPaneFocus(networkID int64, paneType string, paneName string) error {
	switch paneType {
	case "channel":
		return a.SetChannelOpen(networkID, paneName, false)
	case "pm":
		return a.SetPrivateMessageOpen(networkID, paneName, false)
	case "status":
		a.eventBus.Emit(events.Event{
			Type: events.EventUIPaneBlurred,
			Data: map[string]interface{}{
				"networkId": networkID,
				"type":      "status",
				"name":      "status",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceUI,
		})
		return nil
	default:
		return fmt.Errorf("unknown pane type: %s", paneType)
	}
}

// --- Plugin management ---

// PluginInfo represents plugin information for the frontend
type PluginInfo struct {
	Name          string                 `json:"name"`
	Version       string                 `json:"version"`
	Description   string                 `json:"description,omitempty"`
	Author        string                 `json:"author,omitempty"`
	Events        []string               `json:"events,omitempty"`
	Permissions   []string               `json:"permissions,omitempty"`
	MetadataTypes []string               `json:"metadata_types,omitempty"`
	ConfigSchema  map[string]interface{} `json:"config_schema,omitempty"`
	Path          string                 `json:"path"`
	Enabled       bool                   `json:"enabled"`
}

// ListPlugins returns information about all plugins
func (a *App) ListPlugins() []PluginInfo {
	pluginInfos := a.pluginManager.ListPlugins()
	result := make([]PluginInfo, len(pluginInfos))
	for i, p := range pluginInfos {
		result[i] = PluginInfo{
			Name:          p.Name,
			Version:       p.Version,
			Description:   p.Description,
			Author:        p.Author,
			Events:        p.Events,
			Permissions:   p.Permissions,
			MetadataTypes: p.MetadataTypes,
			ConfigSchema:  p.ConfigSchema,
			Path:          p.Path,
			Enabled:       p.Enabled,
		}
	}
	return result
}

// EnablePlugin enables a plugin
func (a *App) EnablePlugin(name string) error {
	return fmt.Errorf("not implemented")
}

// DisablePlugin disables a plugin
func (a *App) DisablePlugin(name string) error {
	return a.pluginManager.UnloadPlugin(name)
}

// ReloadPlugin reloads a plugin
func (a *App) ReloadPlugin(name string) error {
	return a.pluginManager.ReloadPlugin(name)
}

// GetNicknameColor gets color for a single nickname
func (a *App) GetNicknameColor(networkID int64, nickname string) (string, error) {
	return a.pluginManager.GetNicknameColor(networkID, nickname), nil
}

// GetNicknameColorsBatch gets colors for multiple nicknames efficiently
func (a *App) GetNicknameColorsBatch(networkID int64, nicknames []string) (map[string]string, error) {
	return a.pluginManager.GetNicknameColorsBatch(networkID, nicknames), nil
}

// GetPluginConfig gets the configuration for a plugin
func (a *App) GetPluginConfig(pluginName string) (map[string]interface{}, error) {
	config, err := a.storage.GetPluginConfig(pluginName)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin config: %w", err)
	}
	if config.Config == nil {
		return make(map[string]interface{}), nil
	}
	return config.Config, nil
}

// SetPluginConfig saves the configuration for a plugin
func (a *App) SetPluginConfig(pluginName string, config map[string]interface{}) error {
	if err := a.storage.SetPluginConfig(pluginName, config); err != nil {
		return fmt.Errorf("failed to set plugin config: %w", err)
	}
	// Reload the plugin if it's currently loaded to apply new config
	plugins := a.pluginManager.ListPlugins()
	for _, p := range plugins {
		if p.Name == pluginName && p.Enabled {
			if err := a.pluginManager.UnloadPlugin(pluginName); err != nil {
				logger.Log.Warn().Err(err).Str("plugin", pluginName).Msg("Failed to unload plugin for config update")
			}
			if err := a.pluginManager.LoadPlugin(p); err != nil {
				logger.Log.Warn().Err(err).Str("plugin", pluginName).Msg("Failed to reload plugin after config update")
			}
			break
		}
	}
	return nil
}

// GetPluginConfigSchema gets the configuration schema for a plugin
func (a *App) GetPluginConfigSchema(pluginName string) (map[string]interface{}, error) {
	plugins := a.pluginManager.ListPlugins()
	for _, p := range plugins {
		if p.Name == pluginName {
			if p.ConfigSchema != nil {
				return p.ConfigSchema, nil
			}
			return make(map[string]interface{}), nil
		}
	}
	return nil, fmt.Errorf("plugin not found: %s", pluginName)
}

// --- Settings UI ---

// OpenSettings emits an event to open the settings modal
func (a *App) OpenSettings() {
	logger.Log.Debug().Msg("OpenSettings called")
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "open-settings")
	}
}

// OpenSettingsNetworks emits an event to open settings to the networks section
func (a *App) OpenSettingsNetworks() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "open-settings", "networks")
	}
}

// OpenSettingsPlugins emits an event to open settings to the plugins section
func (a *App) OpenSettingsPlugins() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "open-settings", "plugins")
	}
}

// OpenSettingsDisplay emits an event to open settings to the display section
func (a *App) OpenSettingsDisplay() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "open-settings", "display")
	}
}

// SearchMessages performs full-text search across stored messages
func (a *App) SearchMessages(query string, networkID *int64, limit int) ([]storage.SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	return a.storage.SearchMessages(query, networkID, limit)
}

// RequestChannelList sends the LIST command to request available channels from the server
func (a *App) RequestChannelList(networkID int64) error {
	a.mu.RLock()
	client, exists := a.ircClients[networkID]
	a.mu.RUnlock()

	if !exists {
		return fmt.Errorf("network not connected")
	}

	if !client.IsConnected() {
		return fmt.Errorf("network not connected")
	}

	return client.SendRawCommand("LIST")
}

// Greet returns a greeting for the given name (kept for compatibility)
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
