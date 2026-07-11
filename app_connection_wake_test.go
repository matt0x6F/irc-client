package main

import (
	"testing"
	"time"
)

// fakeWakeClient stands in for *irc.IRCClient in the wake-reconnect decision so the
// churn-avoidance logic is testable without a live socket.
type fakeWakeClient struct {
	connected   bool
	alive       bool
	forceCalled bool
}

func (f *fakeWakeClient) IsConnected() bool             { return f.connected }
func (f *fakeWakeClient) ProbeAlive(time.Duration) bool { return f.alive }
func (f *fakeWakeClient) ForceReconnect()               { f.forceCalled = true }

// TestReconnectDeadOnWakeSparesLiveLinks is the wake-churn regression. On a macOS
// maintenance DarkWake the machine is up only briefly, and connections macOS kept
// alive across sleep (TCPKeepAlive) still answer a keepalive PING. Those must NOT be
// force-reconnected: every force-reconnect rejoins channels and prints "matt0x6f
// joined" in ##chat (what vdamewood kept seeing). Only links that fail the probe —
// genuinely dead sockets — get reconnected. An already-disconnected client is left to
// the normal handleConnectionLost path.
func TestReconnectDeadOnWakeSparesLiveLinks(t *testing.T) {
	live := &fakeWakeClient{connected: true, alive: true}
	dead := &fakeWakeClient{connected: true, alive: false}
	gone := &fakeWakeClient{connected: false, alive: false}

	n := reconnectDeadOnWake([]wakeReconnectClient{live, dead, gone}, time.Second)

	if live.forceCalled {
		t.Error("a live link (answered the wake probe) was force-reconnected — this is the ##chat rejoin churn")
	}
	if !dead.forceCalled {
		t.Error("a dead link was not reconnected on wake")
	}
	if gone.forceCalled {
		t.Error("an already-disconnected client should be left to the normal reconnect path, not force-reconnected on wake")
	}
	if n != 1 {
		t.Errorf("expected exactly 1 forced reconnect (the dead link), got %d", n)
	}
}
