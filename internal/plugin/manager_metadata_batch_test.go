package plugin

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
)

func TestLargeMetadataEventIsChunkedBelowIPCFrameLimit(t *testing.T) {
	updates := make([]map[string]interface{}, 1600)
	for i := range updates {
		updates[i] = map[string]interface{}{
			"type":       "nickname_color",
			"key":        fmt.Sprintf("nickname:user-%04d-with-a-long-name", i),
			"value":      "#123456",
			"network_id": 7,
		}
	}

	frames, err := pluginEventNotifications(events.Event{
		Type: events.EventMetadataUpdated,
		Data: map[string]interface{}{"updates": updates},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 2 {
		t.Fatalf("got %d frame, want a chunked delivery", len(frames))
	}

	totalUpdates := 0
	for i, frame := range frames {
		wire, err := json.Marshal(Request{JSONRPC: "2.0", Method: "event", Params: frame})
		if err != nil {
			t.Fatal(err)
		}
		if size := len(wire) + 1; size > maxPluginNotificationBytes {
			t.Fatalf("frame %d is %d bytes, limit %d", i+1, size, maxPluginNotificationBytes)
		}
		items, ok := frame.Data["updates"].([]interface{})
		if !ok {
			t.Fatalf("frame %d updates type = %T", i+1, frame.Data["updates"])
		}
		totalUpdates += len(items)
		if frame.Data["batch_index"] != i+1 || frame.Data["batch_total"] != len(frames) {
			t.Fatalf("frame %d batch markers are wrong", i+1)
		}
	}
	if totalUpdates != len(updates) {
		t.Fatalf("delivered %d updates, want %d", totalUpdates, len(updates))
	}
}

func TestOversizedUnchunkableEventIsRejected(t *testing.T) {
	_, err := pluginEventNotifications(events.Event{
		Type: "large.event",
		Data: map[string]interface{}{"payload": string(make([]byte, maxPluginNotificationBytes))},
	})
	if err == nil {
		t.Fatal("expected oversized unchunkable event to be rejected")
	}
}

func TestHandleMetadataBatchPublishesOneEvent(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()
	got := make(chan events.Event, 1)
	bus.Subscribe(events.EventMetadataUpdated, subscriberFunc(func(event events.Event) { got <- event }))
	pm := NewManager(bus, t.TempDir())

	err := pm.HandleMetadataBatchRequest("colors", map[string]interface{}{
		"updates": []map[string]interface{}{
			{"type": "nickname_color", "key": "nickname:alice", "value": "#111111", "network_id": 7},
			{"type": "nickname_color", "key": "nickname:bob", "value": "#222222", "network_id": 7},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-got:
		updates, ok := event.Data["updates"].([]map[string]interface{})
		if !ok || len(updates) != 2 {
			t.Fatalf("metadata event updates = %#v, want two updates", event.Data["updates"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for coalesced metadata event")
	}

	colors := pm.GetNicknameColorsBatch(7, []string{"alice", "bob"})
	if colors["alice"] != "#111111" || colors["bob"] != "#222222" {
		t.Fatalf("stored colors = %#v", colors)
	}
}

func TestLegacyMetadataWritesAreCoalesced(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()
	got := make(chan events.Event, 2)
	bus.Subscribe(events.EventMetadataUpdated, subscriberFunc(func(event events.Event) { got <- event }))
	pm := NewManager(bus, t.TempDir())

	for _, params := range []map[string]interface{}{
		{"type": "nickname_color", "key": "nickname:alice", "value": "#111111", "network_id": 7},
		{"type": "nickname_color", "key": "nickname:bob", "value": "#222222", "network_id": 7},
	} {
		if err := pm.HandleMetadataRequest("legacy-colors", params); err != nil {
			t.Fatal(err)
		}
	}

	select {
	case event := <-got:
		updates, ok := event.Data["updates"].([]map[string]interface{})
		if !ok || len(updates) != 2 {
			t.Fatalf("legacy writes produced %#v, want one two-update event", event.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for coalesced metadata event")
	}

	select {
	case event := <-got:
		t.Fatalf("legacy writes produced an extra event: %#v", event.Data)
	case <-time.After(3 * metadataEventFlushDelay):
	}
}

func TestMetadataBurstUsesTrailingEdgeCoalescing(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()
	got := make(chan events.Event, 2)
	bus.Subscribe(events.EventMetadataUpdated, subscriberFunc(func(event events.Event) { got <- event }))
	pm := NewManager(bus, t.TempDir())

	pm.queueMetadataEvents([]map[string]interface{}{
		{"type": "nickname_color", "key": "nickname:alice", "value": "#111111", "network_id": 7},
	})
	time.Sleep(metadataEventFlushDelay / 2)
	pm.queueMetadataEvents([]map[string]interface{}{
		{"type": "nickname_color", "key": "nickname:alice", "value": "#222222", "network_id": 7},
		{"type": "nickname_color", "key": "nickname:bob", "value": "#333333", "network_id": 7},
	})

	select {
	case event := <-got:
		updates, ok := event.Data["updates"].([]map[string]interface{})
		if !ok || len(updates) != 2 {
			t.Fatalf("metadata event updates = %#v, want two coalesced updates", event.Data["updates"])
		}
		values := make(map[string]interface{}, len(updates))
		for _, update := range updates {
			values[update["key"].(string)] = update["value"]
		}
		if values["nickname:alice"] != "#222222" {
			t.Fatalf("alice value = %v, want latest value", values["nickname:alice"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for trailing-edge metadata event")
	}

	select {
	case event := <-got:
		t.Fatalf("metadata burst produced an extra event: %#v", event.Data)
	case <-time.After(3 * metadataEventFlushDelay):
	}
}
