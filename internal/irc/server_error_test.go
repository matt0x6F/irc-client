package irc

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
)

func TestDisconnectStatusTextIncludesUnexpectedTransportCause(t *testing.T) {
	if got := disconnectStatusText(false, errors.New("EOF")); got != "Disconnected from server: EOF" {
		t.Fatalf("disconnectStatusText unexpected drop = %q", got)
	}
	if got := disconnectStatusText(false, io.EOF); got != "Disconnected from server: remote host closed the connection (EOF)" {
		t.Fatalf("disconnectStatusText EOF = %q", got)
	}
	if got := disconnectStatusText(true, errors.New("EOF")); got != "Disconnected from server" {
		t.Fatalf("disconnectStatusText deliberate drop = %q", got)
	}
}

type errorEventRecorder struct {
	mu     sync.Mutex
	events []events.Event
}

func (r *errorEventRecorder) OnEvent(e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *errorEventRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// waitForErrorCount polls for the expected event count: the bus delivers on a
// separate dispatcher goroutine, so arrival is asynchronous.
func waitForErrorCount(t *testing.T, r *errorEventRecorder, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.count() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("EventError emitted %d times, want %d", r.count(), n)
}

// A server-initiated ERROR (K-line, kill, link closing) is the only reason the
// user ever gets for the disconnect that follows — surface it in the status
// window and as an error event.
func TestHandleServerError_WritesReasonAndEmits(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	rec := &errorEventRecorder{}
	c.eventBus.Subscribe(EventError, rec)

	c.handleServerError(parse(t, "ERROR :Closing Link: 203.0.113.7 (K-Lined)"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	if !hasStatusLine(msgs, "error", "Server closed the connection: Closing Link: 203.0.113.7 (K-Lined)") {
		t.Fatalf("missing ERROR status line; got %+v", msgs)
	}
	waitForErrorCount(t, rec, 1)
}

// The server also answers our own QUIT with an ERROR ("Closing Link"). When we
// initiated the teardown (abandoned is set by signalQuit before the reply can
// arrive), the handler must stay silent — a user-initiated disconnect is not
// an error.
func TestHandleServerError_SilentOnUserInitiatedQuit(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	rec := &errorEventRecorder{}
	c.eventBus.Subscribe(EventError, rec)

	c.mu.Lock()
	c.abandoned = true
	c.mu.Unlock()

	c.handleServerError(parse(t, "ERROR :Closing Link: 203.0.113.7 (Quit: bye)"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	for _, m := range msgs {
		if m.MessageType == "error" {
			t.Fatalf("abandoned client wrote an ERROR line: %+v", m)
		}
	}
	// Delivery is async: give a wrongly-emitted event time to arrive before
	// declaring silence.
	time.Sleep(100 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("EventError emitted %d times on own quit, want 0", rec.count())
	}
}

func TestHandleServerError_NoParams(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleServerError(parse(t, "ERROR"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	if !hasStatusLine(msgs, "error", "Server closed the connection") {
		t.Fatalf("missing bare ERROR status line; got %+v", msgs)
	}
}

// An unsolicited server ERROR ("Closing Link") is the server telling us it is
// dropping the connection now — most commonly a K-line, a kill, or Libera
// regaining a nick from an unauthenticated session. The handler must drive
// teardown off that explicit signal rather than leaving the client reporting
// itself connected over a socket the server has already closed (which strands
// the UI "Connected" until the ping loop eventually notices, ~KeepAlive later).
func TestHandleServerError_TearsDownConnection(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	c.handleServerError(parse(t, "ERROR :Closing Link: 203.0.113.7 (Nickname regained by services)"))

	if c.IsConnected() {
		t.Fatal("after a server ERROR (link closing) the client must not still report itself connected")
	}
	// abandoned proves teardown went through signalQuit (which also sets the
	// library quit flag), not an incidental connected=false write — so the
	// orphaned Loop cannot ghost-reconnect under a fallback nick.
	c.mu.RLock()
	abandoned := c.abandoned
	c.mu.RUnlock()
	if !abandoned {
		t.Fatal("server ERROR teardown must mark the client abandoned so its Loop cannot ghost-reconnect")
	}
}
