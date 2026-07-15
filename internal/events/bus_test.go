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

// orderedSub records the "i" field of every event and signals once it has seen
// `want` of them.
type orderedSub struct {
	mu    sync.Mutex
	got   []int
	want  int
	done  chan struct{}
	fired bool
}

type blockingSub struct {
	started chan struct{}
	release chan struct{}
}

type typeRecordingSub struct {
	mu     sync.Mutex
	types  []string
	events []Event
	notify chan struct{}
}

func (s *blockingSub) OnEvent(Event) {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-s.release
}

func (s *typeRecordingSub) OnEvent(event Event) {
	s.mu.Lock()
	s.types = append(s.types, event.Type)
	s.events = append(s.events, event)
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *typeRecordingSub) snapshot() ([]string, []Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.types...), append([]Event(nil), s.events...)
}

func (s *orderedSub) OnEvent(e Event) {
	s.mu.Lock()
	s.got = append(s.got, e.Data["i"].(int))
	if len(s.got) == s.want && !s.fired {
		s.fired = true
		close(s.done)
	}
	s.mu.Unlock()
}

func (s *orderedSub) snapshot() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.got...)
}

func (s *orderedSub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

// Emit must deliver events to a subscriber in emit order and lose none. The old
// per-event goroutine dispatch (`go sub.OnEvent(...)`) scrambled ordering under
// load.
func TestEmitPreservesOrderAndDeliversAll(t *testing.T) {
	eb := NewEventBus()
	const n = 200
	rec := &orderedSub{done: make(chan struct{}), want: n}
	eb.Subscribe("seq", rec)

	for i := 0; i < n; i++ {
		eb.Emit(Event{Type: "seq", Data: map[string]interface{}{"i": i}})
	}

	select {
	case <-rec.done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out; delivered %d of %d events", rec.count(), n)
	}

	got := rec.snapshot()
	if len(got) != n {
		t.Fatalf("delivered %d events, want %d", len(got), n)
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("event at position %d delivered out of order: got %d", i, v)
		}
	}
}

func TestEmitLatestCoalescesWithoutBlocking(t *testing.T) {
	eb := NewEventBus()
	defer eb.Close()
	blocker := &blockingSub{started: make(chan struct{}), release: make(chan struct{})}
	eb.Subscribe("gate", blocker)
	recorder := &typeRecordingSub{notify: make(chan struct{}, 1)}
	eb.Subscribe("snapshot", recorder)

	eb.Emit(Event{Type: "gate"})
	<-blocker.started

	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			eb.EmitLatest("same-key", Event{Type: "snapshot", Data: map[string]interface{}{"i": i}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("EmitLatest blocked behind a stalled dispatcher")
	}

	close(blocker.release)
	select {
	case <-recorder.notify:
	case <-time.After(time.Second):
		t.Fatal("coalesced snapshot was not delivered")
	}
	_, got := recorder.snapshot()
	if len(got) != 1 || got[0].Data["i"] != 4999 {
		t.Fatalf("coalesced events = %#v, want only the latest value", got)
	}
}

func TestOrdinaryEventOvertakesLatestBacklog(t *testing.T) {
	eb := NewEventBus()
	defer eb.Close()
	blocker := &blockingSub{started: make(chan struct{}), release: make(chan struct{})}
	eb.Subscribe("gate", blocker)
	recorder := &typeRecordingSub{notify: make(chan struct{}, 4)}
	eb.Subscribe("*", recorder)

	eb.Emit(Event{Type: "gate"})
	<-blocker.started
	for i := 0; i < 100; i++ {
		eb.EmitLatest(string(rune(i+1)), Event{Type: "snapshot"})
	}
	eb.Emit(Event{Type: "control"})
	close(blocker.release)

	deadline := time.After(time.Second)
	for {
		types, _ := recorder.snapshot()
		if len(types) >= 2 {
			if types[0] != "gate" || types[1] != "control" {
				t.Fatalf("delivery order = %v, control event did not overtake snapshots", types[:2])
			}
			return
		}
		select {
		case <-recorder.notify:
		case <-deadline:
			t.Fatal("timed out waiting for control event")
		}
	}
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

func TestEmitAfterCloseDoesNotBlock(t *testing.T) {
	eb := NewEventBus()
	eb.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i <= eventQueueSize; i++ {
			eb.Emit(Event{Type: "after.close"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Emit blocked after EventBus.Close")
	}
}

func TestCloseUnblocksMultipleEmittersWhenQueueIsFull(t *testing.T) {
	eb := NewEventBus()
	sub := &blockingSub{started: make(chan struct{}), release: make(chan struct{})}
	eb.Subscribe("blocked", sub)
	eb.Emit(Event{Type: "blocked"})
	<-sub.started

	for i := 0; i < eventQueueSize; i++ {
		eb.Emit(Event{Type: "blocked"})
	}
	emitDone := make(chan struct{}, 2)
	emitStarted := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			emitStarted <- struct{}{}
			eb.Emit(Event{Type: "blocked"})
			emitDone <- struct{}{}
		}()
	}
	<-emitStarted
	<-emitStarted
	// Give both emitters a scheduling turn against the already-full queue.
	time.Sleep(20 * time.Millisecond)
	closeDone := make(chan struct{})
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		eb.Close()
		close(closeDone)
	}()
	<-closeStarted
	time.Sleep(20 * time.Millisecond)

	close(sub.release)
	for i := 0; i < 2; i++ {
		select {
		case <-emitDone:
		case <-time.After(250 * time.Millisecond):
			t.Fatal("full-queue emitter remained blocked during Close")
		}
	}
	select {
	case <-closeDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Close deadlocked with a full queue")
	}
}
