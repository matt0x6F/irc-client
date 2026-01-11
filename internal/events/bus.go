package events

import (
	"sync"
	"time"
)

// EventSource represents the source of an event
type EventSource string

const (
	EventSourceIRC     EventSource = "irc"
	EventSourceUI      EventSource = "ui"     // Future
	EventSourceSystem  EventSource = "system"
)

// System event types
const (
	EventMetadataUpdated = "metadata.updated"
)

// UI event types
const (
	EventUIPaneFocused = "ui.pane.focused"   // When a pane (channel/PM/status) gains focus
	EventUIPaneBlurred  = "ui.pane.blurred"  // When a pane loses focus
)

// Event represents a generic event
type Event struct {
	Type      string
	Data      map[string]interface{}
	Timestamp time.Time
	Source    EventSource
}

// Subscriber is an interface for event subscribers
type Subscriber interface {
	OnEvent(event Event)
}

// EventBus manages event routing
type EventBus struct {
	subscribers map[string][]Subscriber
	mu          sync.RWMutex
}

// NewEventBus creates a new event bus
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]Subscriber),
	}
}

// Subscribe subscribes a subscriber to a specific event type
func (eb *EventBus) Subscribe(eventType string, subscriber Subscriber) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.subscribers[eventType] = append(eb.subscribers[eventType], subscriber)
}

// Unsubscribe removes a subscriber from an event type
func (eb *EventBus) Unsubscribe(eventType string, subscriber Subscriber) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	subs := eb.subscribers[eventType]
	for i, sub := range subs {
		if sub == subscriber {
			eb.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

// Emit emits an event to all subscribers
func (eb *EventBus) Emit(event Event) {
	eb.mu.RLock()
	subs := make([]Subscriber, len(eb.subscribers[event.Type]))
	copy(subs, eb.subscribers[event.Type])
	eb.mu.RUnlock()

	// Also emit to wildcard subscribers
	eb.mu.RLock()
	wildcardSubs := make([]Subscriber, len(eb.subscribers["*"]))
	copy(wildcardSubs, eb.subscribers["*"])
	eb.mu.RUnlock()

	// Emit to specific subscribers
	for _, sub := range subs {
		go sub.OnEvent(event)
	}

	// Emit to wildcard subscribers
	for _, sub := range wildcardSubs {
		go sub.OnEvent(event)
	}
}

// EmitSync emits an event synchronously (for testing or when order matters)
func (eb *EventBus) EmitSync(event Event) {
	eb.mu.RLock()
	subs := make([]Subscriber, len(eb.subscribers[event.Type]))
	copy(subs, eb.subscribers[event.Type])
	wildcardSubs := make([]Subscriber, len(eb.subscribers["*"]))
	copy(wildcardSubs, eb.subscribers["*"])
	eb.mu.RUnlock()

	for _, sub := range subs {
		sub.OnEvent(event)
	}

	for _, sub := range wildcardSubs {
		sub.OnEvent(event)
	}
}

