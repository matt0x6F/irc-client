package irc

import (
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
)

// inviteEventSink captures events for invite test assertions.
type inviteEventSink struct{ ch chan events.Event }

func (s *inviteEventSink) OnEvent(e events.Event) { s.ch <- e }

// statusBufferHas reports whether the network status buffer (channel:nil) holds a
// row of the given messageType whose text matches want.
func statusBufferHas(t *testing.T, c *IRCClient, messageType, want string) bool {
	t.Helper()
	msgs, err := c.storage.GetMessages(c.networkID, nil, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	for _, m := range msgs {
		if m.MessageType == messageType && m.Message == want {
			return true
		}
	}
	return false
}

func TestHandleInvite_ToMeEmitsEvent(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	// The test network nick is matt0x6f (set in newUserMetaTestClient via network.Nickname).
	sink := &inviteEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventInviteReceived, sink)

	c.handleInvite(parse(t, ":alice!a@h INVITE matt0x6f #chan"))

	select {
	case got := <-sink.ch:
		if got.Type != EventInviteReceived {
			t.Fatalf("want %s, got %q", EventInviteReceived, got.Type)
		}
		if got.Data["inviter"] != "alice" || got.Data["channel"] != "#chan" {
			t.Fatalf("unexpected data: %+v", got.Data)
		}
		if got.Data["networkId"] != c.networkID {
			t.Fatalf("networkId: want %d, got %v", c.networkID, got.Data["networkId"])
		}
		if _, ok := got.Data["receivedAt"].(string); !ok {
			t.Fatal("receivedAt must be an RFC3339 string")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for invite.received event")
	}
}

func TestHandleInvite_ThirdPartyDoesNotEmitInviteEvent(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	sink := &inviteEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventInviteReceived, sink)

	c.handleInvite(parse(t, ":alice!a@h INVITE bob #chan"))

	select {
	case e := <-sink.ch:
		t.Fatalf("third-party invite must not emit invite.received; got %+v", e)
	case <-time.After(100 * time.Millisecond):
		// pass: no event emitted
	}

	// A third-party invite is an informational FYI: it must write a status line.
	if !statusBufferHas(t, c, "status", "alice invited bob to #chan") {
		t.Fatal("third-party invite must write a 'status' line to the status buffer")
	}
}

func TestHandleInvite_IgnoredWithoutParams(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	sink := &inviteEventSink{ch: make(chan events.Event, 4)}
	c.eventBus.Subscribe(EventInviteReceived, sink)

	c.handleInvite(parse(t, ":alice!a@h INVITE"))

	select {
	case e := <-sink.ch:
		t.Fatalf("INVITE with no params must not emit an event; got %+v", e)
	case <-time.After(100 * time.Millisecond):
		// pass
	}
}
