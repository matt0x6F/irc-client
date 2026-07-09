package main

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/irc"
)

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
