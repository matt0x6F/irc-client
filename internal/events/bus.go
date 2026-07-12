package events

import (
	"sync"
	"time"
)

// EventSource represents the source of an event
type EventSource string

const (
	EventSourceIRC    EventSource = "irc"
	EventSourceUI     EventSource = "ui" // Future
	EventSourceSystem EventSource = "system"
)

// System event types
const (
	EventMetadataUpdated = "metadata.updated"
	EventPluginLifecycle = "plugin.lifecycle"
)

// UI event types
const (
	EventUIPaneFocused = "ui.pane.focused" // When a pane (channel/PM/status) gains focus
	EventUIPaneBlurred = "ui.pane.blurred" // When a pane loses focus
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

// EventBus manages event routing. Emit is asynchronous but ordered: events are
// queued on a buffered channel and delivered by a single dispatcher goroutine,
// so subscribers see events in emit order (and goroutine count stays bounded to
// one regardless of event volume).
type EventBus struct {
	subscribers map[string][]Subscriber
	mu          sync.RWMutex
	queue       chan Event
	closeOnce   sync.Once
	done        chan struct{}
	closed      bool
}

// eventQueueSize bounds the in-flight event backlog. Emit blocks only if the
// dispatcher falls this far behind (backpressure onto the emitter), which is
// preferable to unbounded goroutine/memory growth under a flood.
const eventQueueSize = 1024

// NewEventBus creates a new event bus and starts its dispatcher goroutine.
func NewEventBus() *EventBus {
	eb := &EventBus{
		subscribers: make(map[string][]Subscriber),
		queue:       make(chan Event, eventQueueSize),
		done:        make(chan struct{}),
	}
	go eb.dispatchLoop()
	return eb
}

// dispatchLoop delivers queued events to subscribers in order, one at a time.
func (eb *EventBus) dispatchLoop() {
	for {
		select {
		case event := <-eb.queue:
			eb.deliver(event)
		case <-eb.done:
			return
		}
	}
}

// deliver snapshots the subscriber lists and calls them synchronously, in order.
func (eb *EventBus) deliver(event Event) {
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

// Close stops the dispatcher goroutine. Safe to call more than once.
func (eb *EventBus) Close() {
	eb.closeOnce.Do(func() {
		eb.mu.Lock()
		eb.closed = true
		close(eb.done)
		eb.mu.Unlock()
	})
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

// Emit queues an event for ordered, asynchronous delivery to all subscribers.
// Delivery happens on the single dispatcher goroutine, so subscribers observe
// events in the order they were emitted. Blocks only if the queue is full
// (sustained overload) — backpressure onto the emitter.
func (eb *EventBus) Emit(event Event) {
	eb.mu.RLock()
	closed := eb.closed
	eb.mu.RUnlock()
	if closed {
		return
	}

	// Never hold the subscriber/closed-state lock across a potentially blocking
	// queue send. Close signals done, which releases every saturated emitter
	// without waiting for the dispatcher or a slow subscriber to make progress.
	select {
	case eb.queue <- event:
	case <-eb.done:
	}
}

// EmitSync emits an event synchronously (for testing or when order matters)
func (eb *EventBus) EmitSync(event Event) {
	eb.mu.RLock()
	if eb.closed {
		eb.mu.RUnlock()
		return
	}
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
