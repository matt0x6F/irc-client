package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/logger"
)

// Manager manages plugin lifecycle and event routing
type Manager struct {
	plugins     map[string]*Plugin
	eventBus    *events.EventBus
	metadataReg *MetadataRegistry
	pluginDir   string
	mu          sync.RWMutex
	actionQueue chan Action
}

// Plugin represents a running plugin instance
type Plugin struct {
	Info *PluginInfo
	IPC  *IPC
	mu   sync.RWMutex
}

// Action represents an action requested by a plugin
type Action struct {
	PluginID string
	Type     string
	Data     map[string]interface{}
}

// NewManager creates a new plugin manager
func NewManager(eventBus *events.EventBus, pluginDir string) *Manager {
	// Default plugin directory
	if pluginDir == "" {
		homeDir, _ := os.UserHomeDir()
		pluginDir = filepath.Join(homeDir, ".cascade-chat", "plugins")
	}

	pm := &Manager{
		plugins:     make(map[string]*Plugin),
		eventBus:    eventBus,
		metadataReg: NewMetadataRegistry(),
		pluginDir:   pluginDir,
		actionQueue: make(chan Action, 100),
	}

	// Subscribe to events
	eventBus.Subscribe("*", pm)

	return pm
}

// OnEvent implements the Subscriber interface
func (pm *Manager) OnEvent(event events.Event) {
	pm.mu.RLock()
	plugins := make([]*Plugin, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		plugins = append(plugins, plugin)
	}
	pm.mu.RUnlock()

	if len(plugins) == 0 {
		logger.Log.Debug().
			Str("event", event.Type).
			Msg("No plugins loaded, skipping event")
		return
	}

	logger.Log.Info().
		Str("event", event.Type).
		Int("plugin_count", len(plugins)).
		Msg("Plugin manager received event")

	// Send event to all plugins that subscribe to it
	for _, plugin := range plugins {
		// Check if plugin subscribes to this event type
		subscribes := false
		for _, eventType := range plugin.Info.Events {
			if eventType == event.Type || eventType == "*" {
				subscribes = true
				break
			}
		}

		if subscribes {
			// Send event notification
			params := EventParams{
				Type: event.Type,
				Data: event.Data,
			}
			logger.Log.Info().
				Str("plugin", plugin.Info.Name).
				Str("event", event.Type).
				Interface("data", event.Data).
				Msg("Sending event to plugin")
			if err := plugin.IPC.SendNotification("event", params); err != nil {
				logger.Log.Warn().
					Err(err).
					Str("plugin", plugin.Info.Name).
					Str("event", event.Type).
					Msg("Failed to send event to plugin")
			}
		} else {
			logger.Log.Debug().
				Str("plugin", plugin.Info.Name).
				Str("event", event.Type).
				Strs("subscribed_events", plugin.Info.Events).
				Msg("Plugin does not subscribe to this event")
		}
	}
}

// DiscoverAndLoad discovers and loads all available plugins
func (pm *Manager) DiscoverAndLoad() error {
	plugins, err := DiscoverPlugins(pm.pluginDir)
	if err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	logger.Log.Info().Int("count", len(plugins)).Msg("Discovered plugins")

	for _, info := range plugins {
		logger.Log.Info().
			Str("name", info.Name).
			Str("path", info.Path).
			Bool("enabled", info.Enabled).
			Strs("events", info.Events).
			Msg("Found plugin")
		
		if info.Enabled {
			logger.Log.Info().Str("plugin", info.Name).Msg("Attempting to load plugin")
			if err := pm.LoadPlugin(info); err != nil {
				logger.Log.Error().Err(err).Str("plugin", info.Name).Msg("Failed to load plugin")
				continue
			}
			logger.Log.Info().Str("plugin", info.Name).Msg("Successfully loaded plugin")
		} else {
			logger.Log.Info().Str("plugin", info.Name).Msg("Plugin is disabled, skipping")
		}
	}

	return nil
}

// LoadPlugin loads a plugin
func (pm *Manager) LoadPlugin(info *PluginInfo) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if already loaded
	if _, exists := pm.plugins[info.Name]; exists {
		return fmt.Errorf("plugin already loaded: %s", info.Name)
	}

	// Validate plugin
	logger.Log.Debug().
		Str("plugin", info.Name).
		Str("path", info.Path).
		Msg("Validating plugin")
	if err := ValidatePlugin(info.Path); err != nil {
		logger.Log.Error().
			Err(err).
			Str("plugin", info.Name).
			Str("path", info.Path).
			Msg("Plugin validation failed")
		return fmt.Errorf("plugin validation failed: %w", err)
	}
	logger.Log.Debug().
		Str("plugin", info.Name).
		Msg("Plugin validation passed")

	// Create IPC connection
	logger.Log.Debug().
		Str("plugin", info.Name).
		Str("path", info.Path).
		Msg("Creating IPC connection")
	ipc, err := NewIPC(info.Path, info.Name, pm)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("plugin", info.Name).
			Msg("Failed to create IPC connection")
		return fmt.Errorf("failed to create IPC: %w", err)
	}
	logger.Log.Debug().
		Str("plugin", info.Name).
		Msg("IPC connection created successfully")

	plugin := &Plugin{
		Info: info,
		IPC:  ipc,
	}

	// Initialize plugin
	initParams := InitializeParams{
		Version:     "1.0",
		Capabilities: info.Events,
	}

	logger.Log.Info().
		Str("plugin", info.Name).
		Interface("initParams", initParams).
		Msg("Sending initialize request to plugin")

	resp, err := ipc.SendRequest("initialize", initParams)
	if err != nil {
		logger.Log.Error().
			Err(err).
			Str("plugin", info.Name).
			Msg("Failed to send initialize request")
		ipc.Close()
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	if resp.Error != nil {
		logger.Log.Error().
			Str("plugin", info.Name).
			Interface("error", resp.Error).
			Msg("Plugin returned initialization error")
		// Close and kill the plugin process on initialization error
		if closeErr := ipc.Close(); closeErr != nil {
			logger.Log.Warn().
				Err(closeErr).
				Str("plugin", info.Name).
				Msg("Error closing plugin IPC after initialization error")
		}
		return fmt.Errorf("plugin initialization error: %s", resp.Error.Message)
	}

	logger.Log.Info().
		Str("plugin", info.Name).
		Interface("response", resp.Result).
		Msg("Plugin initialized successfully")

	pm.plugins[info.Name] = plugin

	return nil
}

// UnloadPlugin unloads a plugin
func (pm *Manager) UnloadPlugin(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin not loaded: %s", name)
	}

	if err := plugin.IPC.Close(); err != nil {
		return fmt.Errorf("failed to close plugin IPC: %w", err)
	}

	// Clear plugin metadata
	pm.metadataReg.ClearPluginMetadata(name)

	delete(pm.plugins, name)
	return nil
}

// ListPlugins returns information about all plugins
func (pm *Manager) ListPlugins() []*PluginInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	infos := make([]*PluginInfo, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		infos = append(infos, plugin.Info)
	}

	return infos
}

// GetActionQueue returns the action queue channel
func (pm *Manager) GetActionQueue() <-chan Action {
	return pm.actionQueue
}

// ProcessActions processes actions from plugins (should be called in a goroutine)
func (pm *Manager) ProcessActions(actionHandler func(Action) error) {
	for action := range pm.actionQueue {
		if err := actionHandler(action); err != nil {
			logger.Log.Error().Err(err).Str("plugin_id", action.PluginID).Msg("Error processing action from plugin")
		}
	}
}

// HandleMetadataRequest handles UI metadata from plugins
func (pm *Manager) HandleMetadataRequest(pluginID string, params map[string]interface{}) error {
	// Parse metadata request
	mtypeStr, ok := params["type"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid type field")
	}
	mtype := MetadataType(mtypeStr)

	key, ok := params["key"].(string)
	if !ok {
		return fmt.Errorf("missing or invalid key field")
	}

	value := params["value"]
	priority := 0
	if p, ok := params["priority"].(float64); ok {
		priority = int(p)
	}

	// Parse scope
	scope := Scope{Key: key}
	if networkIDRaw, ok := params["network_id"]; ok {
		var id int64
		switch v := networkIDRaw.(type) {
		case float64:
			id = int64(v)
		case int64:
			id = v
		case int:
			id = int64(v)
		default:
			logger.Log.Debug().Interface("network_id", networkIDRaw).Msg("Unexpected network_id type")
		}
		if id != 0 {
			scope.NetworkID = &id
		}
	}
	if channel, ok := params["channel"].(string); ok {
		scope.Channel = &channel
	}

	// Store metadata
	pm.metadataReg.SetMetadata(pluginID, scope, mtype, value, priority)
	logger.Log.Info().
		Str("plugin", pluginID).
		Str("type", string(mtype)).
		Str("key", key).
		Interface("value", value).
		Interface("scope", scope).
		Msg("Stored metadata")

	// Emit event for frontend
	eventData := map[string]interface{}{
		"type":  string(mtype),
		"key":   key,
		"value": value,
	}
	if scope.NetworkID != nil {
		eventData["network_id"] = *scope.NetworkID
	}
	if scope.Channel != nil {
		eventData["channel"] = *scope.Channel
	}
	pm.eventBus.Emit(events.Event{
		Type:      events.EventMetadataUpdated,
		Data:      eventData,
		Timestamp: time.Now(),
		Source:    events.EventSourceSystem,
	})

	return nil
}

// GetMetadata exposes metadata to app layer
func (pm *Manager) GetMetadata(networkID int64, channel *string, key string, mtype MetadataType) interface{} {
	return pm.metadataReg.GetMetadata(networkID, channel, key, mtype)
}

// GetMetadataBatch exposes batch retrieval
func (pm *Manager) GetMetadataBatch(networkID int64, channel *string, keys []string, mtype MetadataType) map[string]interface{} {
	return pm.metadataReg.GetMetadataBatch(networkID, channel, keys, mtype)
}

// GetNicknameColor is a convenience method that doesn't expose plugin types to Wails
func (pm *Manager) GetNicknameColor(networkID int64, nickname string) string {
	key := "nickname:" + nickname
	color := pm.metadataReg.GetMetadata(networkID, nil, key, MetadataTypeNicknameColor)
	if color == nil {
		return ""
	}
	if colorStr, ok := color.(string); ok {
		return colorStr
	}
	return ""
}

// GetNicknameColorsBatch is a convenience method that doesn't expose plugin types to Wails
func (pm *Manager) GetNicknameColorsBatch(networkID int64, nicknames []string) map[string]string {
	keys := make([]string, len(nicknames))
	for i, nick := range nicknames {
		keys[i] = "nickname:" + nick
	}
	metadata := pm.metadataReg.GetMetadataBatch(networkID, nil, keys, MetadataTypeNicknameColor)
	result := make(map[string]string)
	for key, value := range metadata {
		nick := strings.TrimPrefix(key, "nickname:")
		if colorStr, ok := value.(string); ok {
			result[nick] = colorStr
		}
	}
	logger.Log.Debug().
		Int64("networkID", networkID).
		Strs("nicknames", nicknames).
		Int("found", len(result)).
		Msg("GetNicknameColorsBatch")
	return result
}

// Close closes all plugins
func (pm *Manager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var errs []error
	for name, plugin := range pm.plugins {
		if err := plugin.IPC.Close(); err != nil {
			errs = append(errs, fmt.Errorf("plugin %s: %w", name, err))
		}
		// Clear plugin metadata
		pm.metadataReg.ClearPluginMetadata(name)
	}

	close(pm.actionQueue)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing plugins: %v", errs)
	}

	return nil
}

