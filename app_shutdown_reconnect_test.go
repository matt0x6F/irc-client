package main

import (
	"sync/atomic"
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
)

type shutdownEventCounter struct{ count atomic.Int64 }

func (s *shutdownEventCounter) OnEvent(events.Event) { s.count.Add(1) }

// Regression test for the shutdown-spawned ghost sessions: app quit tears every
// network down through writeShutdownDisconnectMessages, whose Disconnect() rides
// the same EventConnectionLost path as an unexpected drop. Without the
// intentional-disconnect marker, handleConnectionLost auto-reconnected every
// auto-connect network FROM THE DYING PROCESS — zombie sessions that raced the
// replacement instance for the user's nick (observed in the field as a
// nick_0 ghost joining channels seconds after an app restart).
func TestShutdownMarksIntentionalDisconnect(t *testing.T) {
	a := newDeleteTestApp(t)
	net := createDeleteTestNetwork(t, a, "shutdown-net")

	client := irc.NewIRCClient(net, a.eventBus, a.storage)
	a.ircClients[net.ID] = client

	a.writeShutdownDisconnectMessages()

	a.mu.Lock()
	intentional := a.intentionalDisconnect[net.ID]
	a.mu.Unlock()
	if !intentional {
		t.Error("shutdown teardown did not mark the disconnect intentional; a dying process will auto-reconnect its networks and race the next instance for the nick")
	}
}

func TestServiceShutdownClosesEventBus(t *testing.T) {
	bus := events.NewEventBus()
	counter := &shutdownEventCounter{}
	bus.Subscribe("late.event", counter)
	a := &App{
		eventBus:              bus,
		ircClients:            make(map[int64]*irc.IRCClient),
		intentionalDisconnect: make(map[int64]bool),
	}

	if err := a.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	bus.EmitSync(events.Event{Type: "late.event"})
	if got := counter.count.Load(); got != 0 {
		t.Fatalf("event bus still dispatched after shutdown: got %d events", got)
	}
}
