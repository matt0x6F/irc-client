package events

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testSubscriber is a simple subscriber for testing
type testSubscriber struct {
	mu     sync.Mutex
	events []Event
}

func (ts *testSubscriber) OnEvent(event Event) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.events = append(ts.events, event)
}

func (ts *testSubscriber) getEvents() []Event {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	copied := make([]Event, len(ts.events))
	copy(copied, ts.events)
	return copied
}

func TestNewEventBus(t *testing.T) {
	eb := NewEventBus()
	if eb == nil {
		t.Fatal("NewEventBus returned nil")
	}
	if eb.subscribers == nil {
		t.Fatal("subscribers map is nil")
	}
}

func TestSubscribeAndEmitSync(t *testing.T) {
	eb := NewEventBus()
	sub := &testSubscriber{}

	eb.Subscribe("test.event", sub)

	event := Event{
		Type:      "test.event",
		Data:      map[string]interface{}{"key": "value"},
		Timestamp: time.Now(),
		Source:    EventSourceSystem,
	}

	eb.EmitSync(event)

	events := sub.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "test.event" {
		t.Errorf("expected event type 'test.event', got '%s'", events[0].Type)
	}
	if events[0].Data["key"] != "value" {
		t.Errorf("expected data key='value', got '%v'", events[0].Data["key"])
	}
	if events[0].Source != EventSourceSystem {
		t.Errorf("expected source 'system', got '%s'", events[0].Source)
	}
}

func TestEmitAsync(t *testing.T) {
	eb := NewEventBus()
	sub := &testSubscriber{}

	eb.Subscribe("async.event", sub)

	event := Event{
		Type:      "async.event",
		Data:      map[string]interface{}{"async": true},
		Timestamp: time.Now(),
		Source:    EventSourceIRC,
	}

	eb.Emit(event)

	// Wait for the async goroutine to complete
	time.Sleep(50 * time.Millisecond)

	events := sub.getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "async.event" {
		t.Errorf("expected event type 'async.event', got '%s'", events[0].Type)
	}
}

func TestNoSubscribers(t *testing.T) {
	eb := NewEventBus()

	// Should not panic when emitting with no subscribers
	event := Event{
		Type:      "unsubscribed.event",
		Data:      nil,
		Timestamp: time.Now(),
		Source:    EventSourceSystem,
	}

	eb.EmitSync(event)
	eb.Emit(event)
}

func TestMultipleSubscribers(t *testing.T) {
	eb := NewEventBus()
	sub1 := &testSubscriber{}
	sub2 := &testSubscriber{}
	sub3 := &testSubscriber{}

	eb.Subscribe("multi.event", sub1)
	eb.Subscribe("multi.event", sub2)
	eb.Subscribe("multi.event", sub3)

	event := Event{
		Type:      "multi.event",
		Data:      map[string]interface{}{"count": 3},
		Timestamp: time.Now(),
		Source:    EventSourceSystem,
	}

	eb.EmitSync(event)

	for i, sub := range []*testSubscriber{sub1, sub2, sub3} {
		events := sub.getEvents()
		if len(events) != 1 {
			t.Errorf("subscriber %d: expected 1 event, got %d", i, len(events))
		}
	}
}

func TestWildcardSubscription(t *testing.T) {
	eb := NewEventBus()
	specificSub := &testSubscriber{}
	wildcardSub := &testSubscriber{}

	eb.Subscribe("specific.event", specificSub)
	eb.Subscribe("*", wildcardSub)

	// Emit a specific event
	event1 := Event{
		Type:      "specific.event",
		Timestamp: time.Now(),
		Source:    EventSourceSystem,
	}
	eb.EmitSync(event1)

	// Emit a different event that wildcard should also catch
	event2 := Event{
		Type:      "other.event",
		Timestamp: time.Now(),
		Source:    EventSourceSystem,
	}
	eb.EmitSync(event2)

	specificEvents := specificSub.getEvents()
	if len(specificEvents) != 1 {
		t.Errorf("specific subscriber: expected 1 event, got %d", len(specificEvents))
	}

	wildcardEvents := wildcardSub.getEvents()
	if len(wildcardEvents) != 2 {
		t.Errorf("wildcard subscriber: expected 2 events, got %d", len(wildcardEvents))
	}
	if len(wildcardEvents) >= 2 {
		if wildcardEvents[0].Type != "specific.event" {
			t.Errorf("wildcard event 0: expected 'specific.event', got '%s'", wildcardEvents[0].Type)
		}
		if wildcardEvents[1].Type != "other.event" {
			t.Errorf("wildcard event 1: expected 'other.event', got '%s'", wildcardEvents[1].Type)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	eb := NewEventBus()
	sub := &testSubscriber{}

	eb.Subscribe("unsub.event", sub)
	eb.EmitSync(Event{Type: "unsub.event", Timestamp: time.Now(), Source: EventSourceSystem})

	if len(sub.getEvents()) != 1 {
		t.Fatal("expected 1 event before unsubscribe")
	}

	eb.Unsubscribe("unsub.event", sub)
	eb.EmitSync(Event{Type: "unsub.event", Timestamp: time.Now(), Source: EventSourceSystem})

	if len(sub.getEvents()) != 1 {
		t.Errorf("expected still 1 event after unsubscribe, got %d", len(sub.getEvents()))
	}
}

func TestUnsubscribeNonexistent(t *testing.T) {
	eb := NewEventBus()
	sub := &testSubscriber{}

	// Should not panic
	eb.Unsubscribe("nonexistent", sub)
}

func TestEventIsolation(t *testing.T) {
	eb := NewEventBus()
	sub1 := &testSubscriber{}
	sub2 := &testSubscriber{}

	eb.Subscribe("event.a", sub1)
	eb.Subscribe("event.b", sub2)

	eb.EmitSync(Event{Type: "event.a", Timestamp: time.Now(), Source: EventSourceSystem})

	if len(sub1.getEvents()) != 1 {
		t.Errorf("sub1 expected 1 event, got %d", len(sub1.getEvents()))
	}
	if len(sub2.getEvents()) != 0 {
		t.Errorf("sub2 expected 0 events, got %d", len(sub2.getEvents()))
	}
}

func TestConcurrentSubscribeAndEmit(t *testing.T) {
	eb := NewEventBus()
	var received atomic.Int64
	var wg sync.WaitGroup

	// Concurrently subscribe many subscribers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub := &testSubscriber{}
			eb.Subscribe("concurrent.event", sub)
		}()
	}
	wg.Wait()

	// Now emit concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eb.Emit(Event{Type: "concurrent.event", Timestamp: time.Now(), Source: EventSourceSystem})
			received.Add(1)
		}()
	}
	wg.Wait()

	// Allow async handlers to finish
	time.Sleep(100 * time.Millisecond)

	if received.Load() != 50 {
		t.Errorf("expected 50 emits, got %d", received.Load())
	}
}

func TestConcurrentEmitSync(t *testing.T) {
	eb := NewEventBus()
	sub := &testSubscriber{}
	eb.Subscribe("sync.concurrent", sub)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			eb.EmitSync(Event{
				Type:      "sync.concurrent",
				Data:      map[string]interface{}{"n": n},
				Timestamp: time.Now(),
				Source:    EventSourceSystem,
			})
		}(i)
	}
	wg.Wait()

	events := sub.getEvents()
	if len(events) != 100 {
		t.Errorf("expected 100 events, got %d", len(events))
	}
}

func TestEventSources(t *testing.T) {
	eb := NewEventBus()
	sub := &testSubscriber{}
	eb.Subscribe("source.test", sub)

	sources := []EventSource{EventSourceIRC, EventSourceUI, EventSourceSystem}
	for _, src := range sources {
		eb.EmitSync(Event{Type: "source.test", Source: src, Timestamp: time.Now()})
	}

	events := sub.getEvents()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	for i, src := range sources {
		if events[i].Source != src {
			t.Errorf("event %d: expected source '%s', got '%s'", i, src, events[i].Source)
		}
	}
}
