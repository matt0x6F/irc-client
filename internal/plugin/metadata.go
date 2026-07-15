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
	Priority  int // Higher = more important
	Timestamp time.Time
}

// MetadataUpdate is one parsed plugin metadata write.
type MetadataUpdate struct {
	Scope    Scope
	Type     MetadataType
	Value    interface{}
	Priority int
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
	mr.setMetadataLocked(pluginID, MetadataUpdate{Scope: scope, Type: mtype, Value: value, Priority: priority}, time.Now())
	mr.mu.Unlock()
}

// SetMetadataBatch applies a snapshot using one registry lock and one log line.
// Large channel rosters can contain hundreds of nicknames; handling each as a
// separate write previously produced a second event flood during connection.
func (mr *MetadataRegistry) SetMetadataBatch(pluginID string, updates []MetadataUpdate) {
	mr.mu.Lock()
	stored := 0
	for _, update := range updates {
		if mr.setMetadataLocked(pluginID, update, time.Now()) {
			stored++
		}
	}
	mr.mu.Unlock()

	logger.Log.Info().
		Str("plugin", pluginID).
		Int("updates", len(updates)).
		Int("stored", stored).
		Msg("MetadataRegistry: Stored metadata batch")
}

// setMetadataLocked stores one update. mr.mu must be held by the caller.
func (mr *MetadataRegistry) setMetadataLocked(pluginID string, update MetadataUpdate, now time.Time) bool {
	scopeKey := update.Scope.String()
	if mr.metadata[scopeKey] == nil {
		mr.metadata[scopeKey] = make(map[MetadataType]*MetadataEntry)
	}

	existing := mr.metadata[scopeKey][update.Type]
	if existing != nil && (update.Priority < existing.Priority ||
		(update.Priority == existing.Priority && !now.After(existing.Timestamp))) {
		return false
	}
	mr.metadata[scopeKey][update.Type] = &MetadataEntry{
		Type:      update.Type,
		Value:     update.Value,
		PluginID:  pluginID,
		Priority:  update.Priority,
		Timestamp: now,
	}
	return true
}

// GetMetadata retrieves metadata, checking scopes in order of specificity
func (mr *MetadataRegistry) GetMetadata(networkID int64, channel *string, key string, mtype MetadataType) interface{} {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return mr.getMetadataLocked(networkID, channel, key, mtype)
}

func (mr *MetadataRegistry) getMetadataLocked(networkID int64, channel *string, key string, mtype MetadataType) interface{} {
	// Try most specific first: network+channel+key
	if channel != nil {
		scope := Scope{NetworkID: &networkID, Channel: channel, Key: key}
		if entry := mr.getEntry(scope, mtype); entry != nil {
			return entry.Value
		}
	}

	// Try network+key
	scope := Scope{NetworkID: &networkID, Key: key}
	if entry := mr.getEntry(scope, mtype); entry != nil {
		return entry.Value
	}

	// Try global+key
	scope = Scope{Key: key}
	if entry := mr.getEntry(scope, mtype); entry != nil {
		return entry.Value
	}
	return nil
}

// GetMetadataBatch retrieves multiple metadata values efficiently
func (mr *MetadataRegistry) GetMetadataBatch(networkID int64, channel *string, keys []string, mtype MetadataType) map[string]interface{} {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	result := make(map[string]interface{})
	for _, key := range keys {
		if value := mr.getMetadataLocked(networkID, channel, key, mtype); value != nil {
			result[key] = value
		}
	}
	logger.Log.Info().
		Int64("networkID", networkID).
		Str("type", string(mtype)).
		Int("requested", len(keys)).
		Int("found", len(result)).
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
