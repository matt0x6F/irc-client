package plugin

import "testing"

func TestEnqueueActionDeliversToQueue(t *testing.T) {
	pm := &Manager{actionQueue: make(chan Action, 1)}
	pm.EnqueueAction(Action{PluginID: "p1", Type: "send_message", Data: map[string]interface{}{"target": "#x", "message": "hi"}})
	select {
	case got := <-pm.GetActionQueue():
		if got.Type != "send_message" || got.Data["target"] != "#x" {
			t.Fatalf("unexpected action: %+v", got)
		}
	default:
		t.Fatal("expected an action on the queue")
	}
}

func TestEnqueueActionDropsWhenFull(t *testing.T) {
	pm := &Manager{actionQueue: make(chan Action, 1)}
	pm.EnqueueAction(Action{PluginID: "p1", Type: "a"})
	// Second enqueue must not block/panic when the buffer is full.
	pm.EnqueueAction(Action{PluginID: "p1", Type: "b"})
}
