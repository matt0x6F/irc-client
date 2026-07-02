package irc

import (
	"sync"
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
)

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
	if rec.count() != 1 {
		t.Fatalf("EventError emitted %d times, want 1", rec.count())
	}
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
