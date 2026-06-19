package plugin

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
)

type subscriberFunc func(events.Event)

func (f subscriberFunc) OnEvent(e events.Event) { f(e) }

func TestEmitPluginLifecycle(t *testing.T) {
	bus := events.NewEventBus()
	got := make(chan events.Event, 1)
	bus.Subscribe(events.EventPluginLifecycle, subscriberFunc(func(e events.Event) { got <- e }))
	pm := &Manager{eventBus: bus}
	pm.emitLifecycle("loaded", "weather")
	select {
	case e := <-got:
		if e.Data["plugin"] != "weather" || e.Data["action"] != "loaded" {
			t.Fatalf("unexpected lifecycle event: %+v", e.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no lifecycle event emitted")
	}
}
