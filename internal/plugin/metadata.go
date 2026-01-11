package plugin

import (
	"fmt"
	"strings"
	"sync"
	"time"
	
	"github.com/matt0x6f/irc-client/internal/logger"
)

// MetadataType represents the type of UI metadata
type MetadataType string

const (
	MetadataTypeNicknameColor MetadataType = "nickname_color"
	MetadataTypeNicknameBadge MetadataType = "nickname_badge" // Future
	MetadataTypeNicknameIcon  MetadataType = "nickname_icon"  // Future
)

// Scope represents where metadata applies
type Scope struct {
	NetworkID *int64  // nil = global
	Channel   *string // nil = network-wide
	Key       string  // e.g., "nickname:Alice"
}

// MetadataEntry represents a single metadata value
type MetadataEntry struct {
	Type      MetadataType
	Value     interface{}
	PluginID  string
	Priority  int       // Higher = more important
	Timestamp time.Time
}

// MetadataRegistry manages UI metadata from plugins
type MetadataRegistry struct {
	// map[scope_string][type] = entry
	metadata map[string]map[MetadataType]*MetadataEntry
	mu       sync.RWMutex
}

// NewMetadataRegistry creates a new metadata registry
func NewMetadataRegistry() *MetadataRegistry {
	return &MetadataRegistry{
		metadata: make(map[string]map[MetadataType]*MetadataEntry),
	}
}

// SetMetadata sets metadata from a plugin
func (mr *MetadataRegistry) SetMetadata(pluginID string, scope Scope, mtype MetadataType, value interface{}, priority int) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	scopeKey := scope.String()
	if mr.metadata[scopeKey] == nil {
		mr.metadata[scopeKey] = make(map[MetadataType]*MetadataEntry)
	}

	// Only update if priority is higher or equal (newer wins on tie)
	existing := mr.metadata[scopeKey][mtype]
	if existing == nil || priority > existing.Priority ||
		(priority == existing.Priority && time.Now().After(existing.Timestamp)) {
		mr.metadata[scopeKey][mtype] = &MetadataEntry{
			Type:      mtype,
			Value:     value,
			PluginID:  pluginID,
			Priority:  priority,
			Timestamp: time.Now(),
		}
		// Debug: log what we stored
		logger.Log.Info().
			Str("scope", scopeKey).
			Str("type", string(mtype)).
			Str("key", scope.Key).
			Interface("value", value).
			Str("plugin", pluginID).
			Msg("MetadataRegistry: Stored metadata")
	}
}

// GetMetadata retrieves metadata, checking scopes in order of specificity
func (mr *MetadataRegistry) GetMetadata(networkID int64, channel *string, key string, mtype MetadataType) interface{} {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	// Try most specific first: network+channel+key
	if channel != nil {
		scope := Scope{NetworkID: &networkID, Channel: channel, Key: key}
		if entry := mr.getEntry(scope, mtype); entry != nil {
			logger.Log.Debug().
				Str("scope", scope.String()).
				Str("type", string(mtype)).
				Msg("Found metadata in network+channel scope")
			return entry.Value
		}
	}

	// Try network+key
	scope := Scope{NetworkID: &networkID, Key: key}
	if entry := mr.getEntry(scope, mtype); entry != nil {
		logger.Log.Debug().
			Str("scope", scope.String()).
			Str("type", string(mtype)).
			Msg("Found metadata in network scope")
		return entry.Value
	}

	// Try global+key
	scope = Scope{Key: key}
	if entry := mr.getEntry(scope, mtype); entry != nil {
		logger.Log.Debug().
			Str("scope", scope.String()).
			Str("type", string(mtype)).
			Msg("Found metadata in global scope")
		return entry.Value
	}

	logger.Log.Debug().
		Int64("networkID", networkID).
		Str("key", key).
		Str("type", string(mtype)).
		Msg("No metadata found for key")
	return nil
}

// GetMetadataBatch retrieves multiple metadata values efficiently
func (mr *MetadataRegistry) GetMetadataBatch(networkID int64, channel *string, keys []string, mtype MetadataType) map[string]interface{} {
	mr.mu.RLock()
	// Debug: log all stored metadata
	if len(mr.metadata) > 0 {
		allScopes := make([]string, 0, len(mr.metadata))
		for scopeKey := range mr.metadata {
			allScopes = append(allScopes, scopeKey)
		}
		logger.Log.Debug().
			Strs("all_stored_scopes", allScopes).
			Msg("All stored metadata scopes")
	}
	mr.mu.RUnlock()
	
	result := make(map[string]interface{})
	for _, key := range keys {
		if value := mr.GetMetadata(networkID, channel, key, mtype); value != nil {
			result[key] = value
		}
	}
	logger.Log.Info().
		Int64("networkID", networkID).
		Strs("keys", keys).
		Str("type", string(mtype)).
		Int("found", len(result)).
		Interface("result", result).
		Msg("GetMetadataBatch result")
	return result
}

// ClearPluginMetadata removes all metadata from a plugin (on unload)
func (mr *MetadataRegistry) ClearPluginMetadata(pluginID string) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	for scopeKey, types := range mr.metadata {
		for mtype, entry := range types {
			if entry.PluginID == pluginID {
				delete(types, mtype)
			}
		}
		if len(types) == 0 {
			delete(mr.metadata, scopeKey)
		}
	}
}

// getEntry retrieves an entry for a specific scope and type (assumes lock is held)
func (mr *MetadataRegistry) getEntry(scope Scope, mtype MetadataType) *MetadataEntry {
	scopeKey := scope.String()
	if types, exists := mr.metadata[scopeKey]; exists {
		if entry, ok := types[mtype]; ok {
			return entry
		}
		logger.Log.Debug().
			Str("scope", scopeKey).
			Str("type", string(mtype)).
			Msg("Scope exists but type not found")
	} else {
		logger.Log.Debug().
			Str("scope", scopeKey).
			Msg("Scope not found in registry")
		// Debug: list all available scopes
		if len(mr.metadata) > 0 {
			keys := make([]string, 0, len(mr.metadata))
			for k := range mr.metadata {
				keys = append(keys, k)
			}
			logger.Log.Debug().
				Strs("available_scopes", keys).
				Msg("Available scopes in registry")
		}
	}
	return nil
}

// String generates a scope key for map lookup
func (s Scope) String() string {
	parts := []string{}
	if s.NetworkID != nil {
		parts = append(parts, fmt.Sprintf("network:%d", *s.NetworkID))
	}
	if s.Channel != nil {
		parts = append(parts, fmt.Sprintf("channel:%s", *s.Channel))
	}
	parts = append(parts, fmt.Sprintf("key:%s", s.Key))
	return strings.Join(parts, ":")
}
