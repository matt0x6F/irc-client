package irc

import (
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
)

// newDeafPipe wires a Connection to a stub server that completes registration but
// then goes SILENT: it keeps draining input so the client's writes never block,
// yet it NEVER closes the socket — not even on QUIT. This reproduces a TCP
// connection killed while the laptop slept: the far end is gone but no FIN/EOF is
// ever delivered, so a client read parked in the netpoller blocks forever. The
// only thing that can free that read is closing the socket locally. (Contrast
// newQuitAwarePipe, whose stub closes on QUIT like a real server.)
func newDeafPipe(t *testing.T) *ircevent.Connection { return newStubServerPipe(t, false) }

// TestForceReconnectFreesDeadSocketOnWake reproduces the overnight-sleep zombie:
// the far end is gone but no EOF is delivered, so the client's read parks forever
// and the connection is stuck reporting "Connected". The system-wake recovery
// (ForceReconnect) must break that deadlock by forcing the socket closed so the
// library Loop tears down — instead of hanging until the far-future ping timeout
// (here 10s, well past a real wake handler's usefulness). conn.Quit() alone
// cannot save it: its QUIT line vanishes into the dead socket and nothing ever
// frees the parked read.
func TestForceReconnectFreesDeadSocketOnWake(t *testing.T) {
	conn := newDeafPipe(t)
	c := &IRCClient{conn: conn, connected: true}
	c.runLoop()

	c.ForceReconnect()

	c.mu.RLock()
	done := c.loopDone
	c.mu.RUnlock()

	select {
	case <-done:
		// Socket forced closed -> parked read freed -> Loop torn down. Recovered.
	case <-time.After(3 * time.Second):
		t.Fatal(`Loop() did not exit after ForceReconnect() on a dead socket — the connection is a zombie stuck "Connected"; nothing force-closed the parked read`)
	}
}
