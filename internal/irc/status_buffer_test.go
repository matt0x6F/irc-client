package irc

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// statusEventSink captures status.message events for live-refresh assertions.
type statusEventSink struct{ ch chan events.Event }

func (s *statusEventSink) OnEvent(e events.Event) { s.ch <- e }

func subscribeStatusEvents(c *IRCClient) *statusEventSink {
	sink := &statusEventSink{ch: make(chan events.Event, 16)}
	c.eventBus.Subscribe(EventStatusMessage, sink)
	return sink
}

func expectStatusEvent(t *testing.T, sink *statusEventSink, networkID int64) {
	t.Helper()
	select {
	case got := <-sink.ch:
		if got.Type != EventStatusMessage {
			t.Fatalf("want %s, got %q", EventStatusMessage, got.Type)
		}
		if got.Data["networkId"] != networkID {
			t.Fatalf("networkId: want %d, got %v", networkID, got.Data["networkId"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for status.message event")
	}
}

func expectNoStatusEvent(t *testing.T, sink *statusEventSink) {
	t.Helper()
	select {
	case e := <-sink.ch:
		t.Fatalf("unexpected status.message event: %+v", e)
	case <-time.After(100 * time.Millisecond):
		// pass
	}
}

// A genuine server notice (bare server-name source, "*" target) stays in the
// Status buffer — and must announce itself so an open server-log pane refreshes
// live instead of only on the next pane re-selection.
func TestServerNoticeEmitsStatusMessage(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	sink := subscribeStatusEvents(c)

	c.handleNotice(parse(t, ":irc.example.org NOTICE * :*** Looking up your hostname..."))

	expectStatusEvent(t, sink, c.networkID)
	if !statusBufferHas(t, c, "notice", "*** Looking up your hostname...") {
		t.Fatal("server notice must be stored in the status buffer")
	}
}

// writeStatusLine must announce the new row as status.message — NOT as a fake
// message.received "privmsg", which flows into the desktop-notification and
// activity classifiers that substring-match the body (status lines regularly
// contain our own nick and would phantom-"mention").
func TestWriteStatusLineEmitsStatusMessageNotMessageReceived(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	sink := subscribeStatusEvents(c)
	received := &statusEventSink{ch: make(chan events.Event, 16)}
	c.eventBus.Subscribe(EventMessageReceived, received)

	c.writeStatusLine("status", "End of WHO for matt0x6f")

	expectStatusEvent(t, sink, c.networkID)
	select {
	case e := <-received.ch:
		t.Fatalf("status line must not emit message.received; got %+v", e)
	case <-time.After(100 * time.Millisecond):
		// pass
	}
}

// writeStatusBuffer is the shared write-and-notify path: the row is committed
// synchronously (readable immediately by the reload the event triggers) and the
// event carries the network id used for pane routing.
func TestWriteStatusBufferWritesRowAndEmits(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	sink := subscribeStatusEvents(c)

	if err := c.writeStatusBuffer(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     "Connected to server",
		MessageType: "status",
		Timestamp:   time.Now(),
	}); err != nil {
		t.Fatalf("writeStatusBuffer: %v", err)
	}

	expectStatusEvent(t, sink, c.networkID)
	if !statusBufferHas(t, c, "status", "Connected to server") {
		t.Fatal("row must be committed before/with the notify")
	}
}

// A service/user notice routes to a query pane (pm_target), not the status
// buffer — it must keep its message.received emission and not fire status.message.
func TestServiceNoticeDoesNotEmitStatusMessage(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	sink := subscribeStatusEvents(c)

	c.handleNotice(parse(t, ":NickServ!ns@services NOTICE matt0x6f :You are now identified"))

	expectNoStatusEvent(t, sink)
}
