package extension

import "sync"

// entry is a registered extension plus its host and event-type subscriptions.
type entry struct {
	ext    *Extension
	host   Host
	events map[string]struct{}
}

// deliverable is an enabled extension subscribed to an event type; the Router
// uses it to fan out without holding the registry lock during delivery.
type deliverable struct {
	id   ID
	host Host
}

// Registry is the runtime-agnostic catalog of extensions.
type Registry struct {
	mu      sync.RWMutex
	entries map[ID]*entry
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[ID]*entry)}
}

// Register adds (or replaces) an extension with its host and subscribed event types.
func (r *Registry) Register(ext *Extension, host Host, eventTypes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	evs := make(map[string]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		evs[t] = struct{}{}
	}
	r.entries[ext.ID] = &entry{ext: ext, host: host, events: evs}
}

// Get returns a snapshot copy of the extension by id.
func (r *Registry) Get(id ID) (Extension, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return Extension{}, false
	}
	return *e.ext, true
}

// List returns a snapshot of all registered extensions.
func (r *Registry) List() []Extension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Extension, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, *e.ext)
	}
	return out
}

// SetEnabled toggles an extension's Enabled flag and reconciles Status.
func (r *Registry) SetEnabled(id ID, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return
	}
	e.ext.Enabled = enabled
	if enabled {
		if e.ext.Status == StatusDisabled {
			e.ext.Status = StatusLoaded
		}
	} else {
		e.ext.Status = StatusDisabled
	}
}

// SetStatus updates an extension's status and error string.
func (r *Registry) SetStatus(id ID, status Status, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		e.ext.Status = status
		e.ext.Err = errMsg
	}
}

// recipients returns the enabled extensions subscribed to eventType.
func (r *Registry) recipients(eventType string) []deliverable {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []deliverable
	for id, e := range r.entries {
		if !e.ext.Enabled {
			continue
		}
		if _, ok := e.events[eventType]; ok {
			out = append(out, deliverable{id: id, host: e.host})
		}
	}
	return out
}
