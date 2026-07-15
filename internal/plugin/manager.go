package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// pluginCommandEntry is a registered plugin command keyed by upper-cased name/alias.
type pluginCommandEntry struct {
	Plugin string
	Spec   CommandSpecWire
}

// Manager manages plugin lifecycle and event routing
type Manager struct {
	plugins          map[string]*Plugin
	eventBus         *events.EventBus
	metadataReg      *MetadataRegistry
	pluginDir        string
	storage          *storage.Storage
	mu               sync.RWMutex
	actionQueue      chan Action
	pluginCommands   map[string]pluginCommandEntry
	isBuiltinCommand func(string) bool
	metadataEventMu  sync.Mutex
	metadataPending  map[string]map[string]interface{}
	metadataFlushSet bool
}

const (
	metadataEventFlushDelay  = 10 * time.Millisecond
	pluginEventChunkOverhead = 128
)

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
		plugins:          make(map[string]*Plugin),
		eventBus:         eventBus,
		metadataReg:      NewMetadataRegistry(),
		pluginDir:        pluginDir,
		actionQueue:      make(chan Action, 100),
		pluginCommands:   make(map[string]pluginCommandEntry),
		isBuiltinCommand: func(string) bool { return false },
		metadataPending:  make(map[string]map[string]interface{}),
	}

	// Subscribe to events
	eventBus.Subscribe("*", pm)

	return pm
}

// allowPATHDiscovery reports whether the user has opted in to discovering
// plugins from the system PATH (setting "plugins.discover_path" == "true").
// PATH discovery is off by default because any cascade-* executable on PATH
// would otherwise auto-load with full user privileges. Absent storage or an
// unset value both mean disabled.
func (pm *Manager) allowPATHDiscovery() bool {
	if pm.storage == nil {
		return false
	}
	v, err := pm.storage.GetSetting("plugins.discover_path")
	if err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to read plugins.discover_path setting; defaulting to PATH discovery off")
		return false
	}
	return v == "true"
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

	params, err := pluginEventNotifications(event)
	if err != nil {
		logger.Log.Warn().Err(err).Str("event", event.Type).Msg("Event cannot be delivered within the plugin IPC frame limit")
		return
	}

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
			logger.Log.Info().
				Str("plugin", plugin.Info.Name).
				Str("event", event.Type).
				Int("frames", len(params)).
				Msg("Sending event to plugin")
			for _, frame := range params {
				if err := plugin.IPC.SendNotification("event", frame); err != nil {
					logger.Log.Warn().
						Err(err).
						Str("plugin", plugin.Info.Name).
						Str("event", event.Type).
						Msg("Failed to send event to plugin")
					break
				}
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

// pluginEventNotifications turns one application event into bounded JSON-RPC
// frames for plugin delivery. Most events stay exactly as they are. Large
// events with a top-level "updates" array are split into independently useful
// chunks, which is the generic batch shape used by the metadata/event system.
func pluginEventNotifications(event events.Event) ([]EventParams, error) {
	params := EventParams{Type: event.Type, Data: event.Data}
	if pluginEventNotificationSize(params) <= maxPluginNotificationBytes {
		return []EventParams{params}, nil
	}

	updates, ok := eventUpdateItems(event.Data["updates"])
	if !ok || len(updates) == 0 {
		return nil, fmt.Errorf("event is too large and has no chunkable updates array")
	}

	// Reserve room for the chunk index/total fields added after chunking.
	target := maxPluginNotificationBytes - pluginEventChunkOverhead
	chunks := make([][]interface{}, 0, 2)
	current := make([]interface{}, 0)
	for _, update := range updates {
		candidate := append(append([]interface{}{}, current...), update)
		data := cloneEventData(event.Data)
		data["updates"] = candidate
		if pluginEventNotificationSize(EventParams{Type: event.Type, Data: data}) <= target {
			current = candidate
			continue
		}
		if len(current) == 0 {
			return nil, fmt.Errorf("one update exceeds the plugin IPC frame limit")
		}
		chunks = append(chunks, current)
		current = []interface{}{update}
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}

	result := make([]EventParams, 0, len(chunks))
	for i, chunk := range chunks {
		data := cloneEventData(event.Data)
		data["updates"] = chunk
		data["batch_index"] = i + 1
		data["batch_total"] = len(chunks)
		frame := EventParams{Type: event.Type, Data: data}
		if size := pluginEventNotificationSize(frame); size > maxPluginNotificationBytes {
			return nil, fmt.Errorf("chunk %d exceeds the plugin IPC frame limit: %d bytes", i+1, size)
		}
		result = append(result, frame)
	}
	return result, nil
}

func pluginEventNotificationSize(params EventParams) int {
	data, err := json.Marshal(Request{JSONRPC: "2.0", Method: "event", Params: params})
	if err != nil {
		return maxPluginNotificationBytes + 1
	}
	return len(data) + 1 // newline-delimited JSON
}

func eventUpdateItems(value interface{}) ([]interface{}, bool) {
	switch updates := value.(type) {
	case []interface{}:
		return updates, true
	case []map[string]interface{}:
		items := make([]interface{}, len(updates))
		for i := range updates {
			items[i] = updates[i]
		}
		return items, true
	default:
		return nil, false
	}
}

func cloneEventData(data map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(data)+2)
	for key, value := range data {
		cloned[key] = value
	}
	return cloned
}

// SetStorage sets the storage instance for the plugin manager
func (pm *Manager) SetStorage(stor *storage.Storage) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.storage = stor
}

// DiscoverAndLoad discovers and loads all available plugins
func (pm *Manager) DiscoverAndLoad() error {
	plugins, err := DiscoverPlugins(pm.pluginDir, pm.allowPATHDiscovery())
	if err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	logger.Log.Info().Int("count", len(plugins)).Msg("Discovered plugins")

	// Load plugin configs from database if storage is available
	var dbConfigs map[string]*storage.PluginConfig
	if pm.storage != nil {
		dbConfigs, err = pm.storage.GetAllPluginConfigs()
		if err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to load plugin configs from database, using defaults")
			dbConfigs = make(map[string]*storage.PluginConfig)
		}
	} else {
		dbConfigs = make(map[string]*storage.PluginConfig)
	}

	for _, info := range plugins {
		// Apply database config (database takes precedence for enabled)
		if dbConfig, exists := dbConfigs[info.Name]; exists {
			info.Enabled = dbConfig.Enabled
			logger.Log.Debug().
				Str("name", info.Name).
				Bool("enabled_from_db", dbConfig.Enabled).
				Msg("Using enabled state from database")
		}

		logger.Log.Info().
			Str("name", info.Name).
			Str("path", info.Path).
			Bool("enabled", info.Enabled).
			Strs("events", info.Events).
			Strs("metadata_types", info.MetadataTypes).
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

	// Load user config from database
	var userConfig map[string]interface{}
	if pm.storage != nil {
		dbConfig, err := pm.storage.GetPluginConfig(info.Name)
		if err == nil && dbConfig.Config != nil {
			userConfig = dbConfig.Config
			logger.Log.Debug().
				Str("plugin", info.Name).
				Interface("config", userConfig).
				Msg("Loaded user config from database")
		} else {
			userConfig = make(map[string]interface{})
		}
	} else {
		userConfig = make(map[string]interface{})
	}

	// Initialize plugin
	initParams := InitializeParams{
		Version:      "1.0",
		Capabilities: []string{}, // Events unknown until plugin responds
		Config:       userConfig,
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

	// Parse metadata from initialize response
	var initResult InitializeResult
	if resp.Result != nil {
		resultBytes, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(resultBytes, &initResult); err == nil {
			// Update plugin info with metadata from initialize response
			if initResult.Name != "" {
				info.Name = initResult.Name
			}
			if initResult.Version != "" {
				info.Version = initResult.Version
			}
			info.Description = initResult.Description
			info.Author = initResult.Author
			info.Events = initResult.Events
			info.MetadataTypes = initResult.MetadataTypes
			info.ConfigSchema = initResult.ConfigSchema
			info.Commands = initResult.Commands
			// Update the plugin's Info
			plugin.Info = info
			// Store config_schema in database for later retrieval
			if pm.storage != nil && initResult.ConfigSchema != nil {
				if err := pm.storage.SetPluginConfigSchema(info.Name, initResult.ConfigSchema); err != nil {
					logger.Log.Warn().Err(err).Str("plugin", info.Name).Msg("Failed to save plugin config_schema to database")
				} else {
					logger.Log.Debug().Str("plugin", info.Name).Msg("Saved plugin config_schema to database")
				}
			}
			logger.Log.Info().
				Str("plugin", info.Name).
				Str("version", info.Version).
				Strs("events", info.Events).
				Msg("Updated plugin info from initialize response")
		} else {
			logger.Log.Warn().
				Err(err).
				Str("plugin", info.Name).
				Msg("Failed to parse initialize result metadata")
		}
	}

	logger.Log.Info().
		Str("plugin", info.Name).
		Interface("response", resp.Result).
		Msg("Plugin initialized successfully")

	pm.registerPluginCommands(info.Name, initResult.Commands, pm.isBuiltinCommand)
	pm.plugins[info.Name] = plugin
	pm.emitLifecycle("loaded", info.Name)

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
	pm.unregisterPluginCommands(name)
	pm.emitLifecycle("unloaded", name)
	return nil
}

// ReloadPlugin reloads a plugin by unloading it (if loaded) and loading it again
func (pm *Manager) ReloadPlugin(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Discover the plugin to get its current info
	plugins, err := DiscoverPlugins(pm.pluginDir, pm.allowPATHDiscovery())
	if err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	var pluginInfo *PluginInfo
	for _, p := range plugins {
		if p.Name == name {
			pluginInfo = p
			break
		}
	}

	if pluginInfo == nil {
		return fmt.Errorf("plugin not found: %s", name)
	}

	// Unload if currently loaded
	if _, exists := pm.plugins[name]; exists {
		if err := pm.unloadPluginUnlocked(name); err != nil {
			logger.Log.Warn().Err(err).Str("plugin", name).Msg("Failed to unload plugin during reload")
			// Continue anyway - try to load the new version
		}
	}

	// Load the plugin
	// Get enabled state from database if available
	if pm.storage != nil {
		dbConfig, err := pm.storage.GetPluginConfig(name)
		if err == nil {
			pluginInfo.Enabled = dbConfig.Enabled
		}
	}

	// Only load if enabled
	if pluginInfo.Enabled {
		return pm.loadPluginUnlocked(pluginInfo)
	}

	return nil
}

// unloadPluginUnlocked unloads a plugin without acquiring the lock (caller must hold lock)
func (pm *Manager) unloadPluginUnlocked(name string) error {
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
	pm.unregisterPluginCommands(name)
	pm.emitLifecycle("unloaded", name)
	return nil
}

// registerPluginCommands registers commands declared by a plugin.
// Skips commands that shadow built-ins or are already owned by another plugin (first-wins policy).
// Must be called under pm.mu (does NOT acquire it internally).
func (pm *Manager) registerPluginCommands(pluginName string, cmds []CommandSpecWire, isBuiltin func(string) bool) {
	for _, c := range cmds {
		for _, key := range append([]string{c.Name}, c.Aliases...) {
			k := strings.ToUpper(key)
			if isBuiltin(k) {
				logger.Log.Warn().Str("plugin", pluginName).Str("command", k).Msg("Plugin command shadows a built-in; ignored")
				continue
			}
			if existing, taken := pm.pluginCommands[k]; taken {
				logger.Log.Warn().Str("plugin", pluginName).Str("command", k).Str("owner", existing.Plugin).Msg("Plugin command already registered; ignored")
				continue
			}
			pm.pluginCommands[k] = pluginCommandEntry{Plugin: pluginName, Spec: c}
		}
	}
}

// unregisterPluginCommands removes all command keys owned by pluginName.
// Must be called under pm.mu (does NOT acquire it internally).
func (pm *Manager) unregisterPluginCommands(pluginName string) {
	for k, e := range pm.pluginCommands {
		if e.Plugin == pluginName {
			delete(pm.pluginCommands, k)
		}
	}
}

// LookupPluginCommand looks up a plugin command by name (case-insensitive).
func (pm *Manager) LookupPluginCommand(name string) (pluginCommandEntry, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	e, ok := pm.pluginCommands[strings.ToUpper(name)]
	return e, ok
}

// PluginCommands returns one entry per canonical command (keyed by Spec.Name, deduped across aliases).
func (pm *Manager) PluginCommands() []pluginCommandEntry {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	seen := map[string]bool{}
	out := []pluginCommandEntry{}
	for _, e := range pm.pluginCommands {
		if seen[strings.ToUpper(e.Spec.Name)] {
			continue
		}
		seen[strings.ToUpper(e.Spec.Name)] = true
		out = append(out, e)
	}
	return out
}

// GetPluginMetadata initializes a plugin temporarily to get its metadata (including config_schema)
// without fully loading it. This is used to discover plugin capabilities before enabling.
func (pm *Manager) GetPluginMetadata(info *PluginInfo) (*PluginInfo, error) {
	// Check if already loaded
	pm.mu.RLock()
	if plugin, exists := pm.plugins[info.Name]; exists {
		pm.mu.RUnlock()
		return plugin.Info, nil
	}
	pm.mu.RUnlock()

	// Check database first
	if pm.storage != nil {
		dbConfig, err := pm.storage.GetPluginConfig(info.Name)
		if err == nil && dbConfig.ConfigSchema != nil && len(dbConfig.ConfigSchema) > 0 {
			// Return metadata from database
			result := &PluginInfo{
				Name:         info.Name,
				Path:         info.Path,
				Enabled:      dbConfig.Enabled,
				ConfigSchema: dbConfig.ConfigSchema,
			}
			return result, nil
		}
	}

	// Need to initialize plugin to get metadata
	// Create temporary IPC connection
	ipc, err := NewIPC(info.Path, info.Name, pm)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPC: %w", err)
	}
	defer ipc.Close()

	// Initialize plugin with empty config
	initParams := InitializeParams{
		Version:      "1.0",
		Capabilities: []string{},
		Config:       make(map[string]interface{}),
	}

	resp, err := ipc.SendRequest("initialize", initParams)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize plugin: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("plugin initialization error: %s", resp.Error.Message)
	}

	// Parse metadata from initialize response
	var initResult InitializeResult
	if resp.Result != nil {
		resultBytes, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(resultBytes, &initResult); err != nil {
			return nil, fmt.Errorf("failed to parse initialize result: %w", err)
		}
	}

	// Build result
	result := &PluginInfo{
		Name:          initResult.Name,
		Version:       initResult.Version,
		Description:   initResult.Description,
		Author:        initResult.Author,
		Events:        initResult.Events,
		MetadataTypes: initResult.MetadataTypes,
		ConfigSchema:  initResult.ConfigSchema,
		Path:          info.Path,
		Enabled:       info.Enabled,
	}

	// Store config_schema in database for future use
	if pm.storage != nil && initResult.ConfigSchema != nil {
		if err := pm.storage.SetPluginConfigSchema(result.Name, initResult.ConfigSchema); err != nil {
			logger.Log.Warn().Err(err).Str("plugin", result.Name).Msg("Failed to save plugin config_schema to database")
		}
	}

	return result, nil
}

// ListPlugins returns information about all discovered plugins (not just loaded ones)
// For loaded plugins, returns full metadata from initialize response.
// For unloaded plugins, returns minimal info (name, path, enabled state).
// If a plugin has config_schema in the database, it will be included.
func (pm *Manager) ListPlugins() []*PluginInfo {
	// Discover all plugins
	plugins, err := DiscoverPlugins(pm.pluginDir, pm.allowPATHDiscovery())
	if err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to discover plugins for list")
		// Fall back to just loaded plugins
		pm.mu.RLock()
		defer pm.mu.RUnlock()
		infos := make([]*PluginInfo, 0, len(pm.plugins))
		for _, plugin := range pm.plugins {
			infos = append(infos, plugin.Info)
		}
		return infos
	}

	// Load plugin configs from database if storage is available
	var dbConfigs map[string]*storage.PluginConfig
	if pm.storage != nil {
		dbConfigs, err = pm.storage.GetAllPluginConfigs()
		if err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to load plugin configs from database")
			dbConfigs = make(map[string]*storage.PluginConfig)
		}
	} else {
		dbConfigs = make(map[string]*storage.PluginConfig)
	}

	// Get loaded plugins with full metadata
	pm.mu.RLock()
	loadedPlugins := make(map[string]*PluginInfo)
	for name, plugin := range pm.plugins {
		// Create a copy to avoid race conditions
		loadedInfo := *plugin.Info
		loadedPlugins[name] = &loadedInfo
	}
	pm.mu.RUnlock()

	infos := make([]*PluginInfo, 0, len(plugins))
	for _, info := range plugins {
		// If plugin is loaded, use its full metadata
		if loadedInfo, exists := loadedPlugins[info.Name]; exists {
			// Copy enabled state from database if available
			if dbConfig, exists := dbConfigs[info.Name]; exists {
				loadedInfo.Enabled = dbConfig.Enabled
			} else {
				loadedInfo.Enabled = true // Plugin is loaded, so it's enabled
			}
			infos = append(infos, loadedInfo)
		} else {
			// Unloaded plugin - use info from discovery, but merge with database
			unloadedInfo := &PluginInfo{
				Name:    info.Name,
				Path:    info.Path,
				Enabled: true, // Default
			}
			// Apply database config if available (including config_schema)
			if dbConfig, exists := dbConfigs[info.Name]; exists {
				unloadedInfo.Enabled = dbConfig.Enabled
				// Include config_schema from database if available
				if dbConfig.ConfigSchema != nil && len(dbConfig.ConfigSchema) > 0 {
					unloadedInfo.ConfigSchema = dbConfig.ConfigSchema
				}
			}

			// Do not execute disabled plugins just to populate optional metadata. A
			// disabled plugin must remain inert; cached schemas are still shown when
			// available from a previous successful initialization.
			if unloadedInfo.Enabled && (unloadedInfo.ConfigSchema == nil || len(unloadedInfo.ConfigSchema) == 0) {
				logger.Log.Debug().Str("plugin", info.Name).Msg("No config_schema in database, fetching from plugin")
				if metadata, err := pm.GetPluginMetadata(info); err == nil && metadata.ConfigSchema != nil {
					logger.Log.Debug().Str("plugin", info.Name).Msg("Successfully fetched config_schema from plugin")
					unloadedInfo.ConfigSchema = metadata.ConfigSchema
					// Also update other metadata if available
					if metadata.Description != "" {
						unloadedInfo.Description = metadata.Description
					}
					if metadata.Version != "" {
						unloadedInfo.Version = metadata.Version
					}
					if metadata.Author != "" {
						unloadedInfo.Author = metadata.Author
					}
				} else if err != nil {
					logger.Log.Warn().Err(err).Str("plugin", info.Name).Msg("Failed to get plugin metadata")
				} else {
					logger.Log.Debug().Str("plugin", info.Name).Msg("Plugin metadata fetched but no config_schema found")
				}
			}

			infos = append(infos, unloadedInfo)
		}
	}

	return infos
}

// SetBuiltinCommandChecker wires the predicate used to block plugin commands from
// shadowing built-ins. Must be called before any plugins are loaded.
func (pm *Manager) SetBuiltinCommandChecker(fn func(string) bool) {
	if fn != nil {
		pm.isBuiltinCommand = fn
	}
}

// GetActionQueue returns the action queue channel
func (pm *Manager) GetActionQueue() <-chan Action {
	return pm.actionQueue
}

// EnqueueAction queues a plugin-requested action for the App to process.
// Non-blocking: if the queue is full the action is dropped with a warning.
func (pm *Manager) EnqueueAction(a Action) {
	select {
	case pm.actionQueue <- a:
	default:
		logger.Log.Warn().Str("plugin", a.PluginID).Str("type", a.Type).Msg("Plugin action queue full; dropping action")
	}
}

// InvokePluginCommand sends a command.invoke request to the owning plugin.
func (pm *Manager) InvokePluginCommand(pluginName, command string, args []string, networkID int64, channel string) error {
	pm.mu.RLock()
	plugin, ok := pm.plugins[pluginName]
	pm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin not loaded: %s", pluginName)
	}
	resp, err := plugin.IPC.SendRequest("command.invoke", map[string]interface{}{
		"command":   command,
		"args":      args,
		"networkId": networkID,
		"channel":   channel,
	})
	if err != nil {
		return fmt.Errorf("plugin command failed: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("plugin command error: %s", resp.Error.Message)
	}
	return nil
}

// ProcessActions processes actions from plugins (should be called in a goroutine)
func (pm *Manager) ProcessActions(actionHandler func(Action) error) {
	for action := range pm.actionQueue {
		if err := actionHandler(action); err != nil {
			logger.Log.Error().Err(err).Str("plugin_id", action.PluginID).Msg("Error processing action from plugin")
		}
	}
}

// emitLifecycle emits a plugin.lifecycle event for load/unload transitions.
// Safe to call while holding pm.mu because eventBus.Emit is non-blocking and
// dispatches asynchronously; callers in load/unload paths hold the lock and
// this is intentional.
func (pm *Manager) emitLifecycle(action, plugin string) {
	if pm.eventBus == nil {
		return
	}
	pm.eventBus.Emit(events.Event{
		Type:      events.EventPluginLifecycle,
		Data:      map[string]interface{}{"action": action, "plugin": plugin},
		Timestamp: time.Now(),
		Source:    events.EventSourceSystem,
	})
}

// HandleMetadataRequest handles UI metadata from plugins
func (pm *Manager) HandleMetadataRequest(pluginID string, params map[string]interface{}) error {
	update, eventData, err := parseMetadataUpdate(params)
	if err != nil {
		return err
	}

	pm.metadataReg.SetMetadata(pluginID, update.Scope, update.Type, update.Value, update.Priority)
	pm.queueMetadataEvents([]map[string]interface{}{eventData})
	return nil
}

// HandleMetadataBatchRequest stores and publishes many metadata values as one
// snapshot, avoiding one event-bus entry per nickname for large NAMES lists.
func (pm *Manager) HandleMetadataBatchRequest(pluginID string, params map[string]interface{}) error {
	raw, ok := params["updates"]
	if !ok {
		return fmt.Errorf("missing updates field")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal metadata updates: %w", err)
	}
	var entries []map[string]interface{}
	if err := json.Unmarshal(encoded, &entries); err != nil {
		return fmt.Errorf("invalid updates field: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	updates := make([]MetadataUpdate, 0, len(entries))
	eventUpdates := make([]map[string]interface{}, 0, len(entries))
	for i, entry := range entries {
		update, eventData, err := parseMetadataUpdate(entry)
		if err != nil {
			return fmt.Errorf("invalid update %d: %w", i, err)
		}
		updates = append(updates, update)
		eventUpdates = append(eventUpdates, eventData)
	}

	pm.metadataReg.SetMetadataBatch(pluginID, updates)
	pm.queueMetadataEvents(eventUpdates)
	return nil
}

// queueMetadataEvents coalesces plugin metadata writes before publishing them
// to the shared event bus. This protects the IRC read path even when an older
// plugin sends hundreds of ui_metadata.set notifications one at a time.
func (pm *Manager) queueMetadataEvents(updates []map[string]interface{}) {
	pm.metadataEventMu.Lock()
	if pm.metadataPending == nil {
		pm.metadataPending = make(map[string]map[string]interface{})
	}
	for _, update := range updates {
		pm.metadataPending[metadataEventKey(update)] = update
	}
	if pm.metadataFlushSet {
		pm.metadataEventMu.Unlock()
		return
	}
	pm.metadataFlushSet = true
	pm.metadataEventMu.Unlock()
	time.AfterFunc(metadataEventFlushDelay, pm.flushMetadataEvents)
}

func metadataEventKey(update map[string]interface{}) string {
	return fmt.Sprintf("%v\x00%v\x00%v\x00%v", update["network_id"], update["channel"], update["type"], update["key"])
}

func (pm *Manager) flushMetadataEvents() {
	pm.metadataEventMu.Lock()
	updates := make([]map[string]interface{}, 0, len(pm.metadataPending))
	for _, update := range pm.metadataPending {
		updates = append(updates, update)
	}
	pm.metadataPending = make(map[string]map[string]interface{})
	pm.metadataFlushSet = false
	pm.metadataEventMu.Unlock()

	if len(updates) == 0 || pm.eventBus == nil {
		return
	}
	logger.Log.Debug().Int("updates", len(updates)).Msg("Publishing coalesced metadata updates")
	data := updates[0]
	if len(updates) > 1 {
		data = map[string]interface{}{"updates": updates}
	}
	pm.eventBus.Emit(events.Event{
		Type:      events.EventMetadataUpdated,
		Data:      data,
		Timestamp: time.Now(),
		Source:    events.EventSourceSystem,
	})
}

func parseMetadataUpdate(params map[string]interface{}) (MetadataUpdate, map[string]interface{}, error) {
	mtypeStr, ok := params["type"].(string)
	if !ok {
		return MetadataUpdate{}, nil, fmt.Errorf("missing or invalid type field")
	}
	mtype := MetadataType(mtypeStr)

	key, ok := params["key"].(string)
	if !ok {
		return MetadataUpdate{}, nil, fmt.Errorf("missing or invalid key field")
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
	return MetadataUpdate{Scope: scope, Type: mtype, Value: value, Priority: priority}, eventData, nil
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

// SetPluginEnabled enables or disables a plugin and persists the state to the database
func (pm *Manager) SetPluginEnabled(name string, enabled bool, stor *storage.Storage) error {
	if stor == nil {
		return fmt.Errorf("storage is required to set plugin enabled state")
	}

	previousConfig, err := stor.GetPluginConfig(name)
	if err != nil {
		return fmt.Errorf("failed to read current plugin config: %w", err)
	}
	previousEnabled := previousConfig.Enabled

	// Save to database
	if err := stor.SetPluginEnabled(name, enabled); err != nil {
		return fmt.Errorf("failed to save plugin config: %w", err)
	}
	restoreEnabledState := func(cause error) error {
		if restoreErr := stor.SetPluginEnabled(name, previousEnabled); restoreErr != nil {
			return fmt.Errorf("%w (also failed to restore enabled state: %v)", cause, restoreErr)
		}
		return cause
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if enabled {
		// Check if plugin is already loaded
		if _, exists := pm.plugins[name]; exists {
			logger.Log.Debug().Str("plugin", name).Msg("Plugin already loaded")
			return nil
		}

		// Discover the plugin to get its info
		plugins, err := DiscoverPlugins(pm.pluginDir, pm.allowPATHDiscovery())
		if err != nil {
			return restoreEnabledState(fmt.Errorf("failed to discover plugins: %w", err))
		}

		// Find the plugin by name
		var pluginInfo *PluginInfo
		for _, p := range plugins {
			if p.Name == name {
				pluginInfo = p
				break
			}
		}

		if pluginInfo == nil {
			return restoreEnabledState(fmt.Errorf("plugin not found: %s", name))
		}

		// Set enabled state (database config takes precedence)
		pluginInfo.Enabled = enabled

		// Load the plugin
		if err := pm.loadPluginUnlocked(pluginInfo); err != nil {
			return restoreEnabledState(fmt.Errorf("failed to load plugin: %w", err))
		}

		logger.Log.Info().Str("plugin", name).Msg("Plugin enabled and loaded")
	} else {
		// Disable: unload plugin and clear its metadata
		if plugin, exists := pm.plugins[name]; exists {
			if err := plugin.IPC.Close(); err != nil {
				logger.Log.Warn().Err(err).Str("plugin", name).Msg("Error closing plugin IPC")
			}
			delete(pm.plugins, name)
			pm.unregisterPluginCommands(name)
			pm.metadataReg.ClearPluginMetadata(name)
			pm.emitLifecycle("unloaded", name)
			logger.Log.Info().Str("plugin", name).Msg("Plugin disabled and unloaded")
		} else {
			logger.Log.Debug().Str("plugin", name).Msg("Plugin not loaded, nothing to unload")
		}
	}

	return nil
}

// loadPluginUnlocked loads a plugin without acquiring the lock (caller must hold lock)
func (pm *Manager) loadPluginUnlocked(info *PluginInfo) error {
	// Check if already loaded
	if _, exists := pm.plugins[info.Name]; exists {
		return fmt.Errorf("plugin already loaded: %s", info.Name)
	}

	// Validate plugin
	if err := ValidatePlugin(info.Path); err != nil {
		return fmt.Errorf("plugin validation failed: %w", err)
	}

	// Create IPC connection
	ipc, err := NewIPC(info.Path, info.Name, pm)
	if err != nil {
		return fmt.Errorf("failed to create IPC: %w", err)
	}

	plugin := &Plugin{
		Info: info,
		IPC:  ipc,
	}

	// Load user config from database
	var userConfig map[string]interface{}
	if pm.storage != nil {
		dbConfig, err := pm.storage.GetPluginConfig(info.Name)
		if err == nil && dbConfig.Config != nil {
			userConfig = dbConfig.Config
		} else {
			userConfig = make(map[string]interface{})
		}
	} else {
		userConfig = make(map[string]interface{})
	}

	// Initialize plugin
	initParams := InitializeParams{
		Version:      "1.0",
		Capabilities: []string{}, // Events unknown until plugin responds
		Config:       userConfig,
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

	// Parse metadata from initialize response
	var initResult InitializeResult
	if resp.Result != nil {
		resultBytes, _ := json.Marshal(resp.Result)
		if err := json.Unmarshal(resultBytes, &initResult); err == nil {
			// Update plugin info with metadata from initialize response
			if initResult.Name != "" {
				info.Name = initResult.Name
			}
			if initResult.Version != "" {
				info.Version = initResult.Version
			}
			info.Description = initResult.Description
			info.Author = initResult.Author
			info.Events = initResult.Events
			info.MetadataTypes = initResult.MetadataTypes
			info.ConfigSchema = initResult.ConfigSchema
			info.Commands = initResult.Commands
			// Update the plugin's Info
			plugin.Info = info
			// Store config_schema in database for later retrieval
			if pm.storage != nil && initResult.ConfigSchema != nil {
				if err := pm.storage.SetPluginConfigSchema(info.Name, initResult.ConfigSchema); err != nil {
					logger.Log.Warn().Err(err).Str("plugin", info.Name).Msg("Failed to save plugin config_schema to database")
				}
			}
		}
	}

	pm.registerPluginCommands(info.Name, initResult.Commands, pm.isBuiltinCommand)
	pm.plugins[info.Name] = plugin
	pm.emitLifecycle("loaded", info.Name)
	return nil
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
