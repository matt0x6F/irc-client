package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/logger"
)

// Manager manages plugin lifecycle and event routing
type Manager struct {
	plugins     map[string]*Plugin
	eventBus    *events.EventBus
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
			plugin.IPC.SendNotification("event", params)
		}
	}
}

// DiscoverAndLoad discovers and loads all available plugins
func (pm *Manager) DiscoverAndLoad() error {
	plugins, err := DiscoverPlugins(pm.pluginDir)
	if err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	for _, info := range plugins {
		if info.Enabled {
			if err := pm.LoadPlugin(info); err != nil {
				logger.Log.Warn().Err(err).Str("plugin", info.Name).Msg("Failed to load plugin")
				continue
			}
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
	if err := ValidatePlugin(info.Path); err != nil {
		return fmt.Errorf("plugin validation failed: %w", err)
	}

	// Create IPC connection
	ipc, err := NewIPC(info.Path)
	if err != nil {
		return fmt.Errorf("failed to create IPC: %w", err)
	}

	plugin := &Plugin{
		Info: info,
		IPC:  ipc,
	}

	// Initialize plugin
	initParams := InitializeParams{
		Version:     "1.0",
		Capabilities: info.Events,
	}

	resp, err := ipc.SendRequest("initialize", initParams)
	if err != nil {
		ipc.Close()
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	if resp.Error != nil {
		ipc.Close()
		return fmt.Errorf("plugin initialization error: %s", resp.Error.Message)
	}

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

// Close closes all plugins
func (pm *Manager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var errs []error
	for name, plugin := range pm.plugins {
		if err := plugin.IPC.Close(); err != nil {
			errs = append(errs, fmt.Errorf("plugin %s: %w", name, err))
		}
	}

	close(pm.actionQueue)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing plugins: %v", errs)
	}

	return nil
}

