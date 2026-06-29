package irc

import (
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
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

func TestHandleInviting_RPL341WritesConfirmation(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// 341 RPL_INVITING: "<me> <nick> <channel>"
	msg := ircmsg.MakeMessage(nil, "server", "341", "me", "bob", "#chan")
	c.handleInviting(msg)

	if !statusBufferHas(t, c, "status", "Invited bob to #chan") {
		t.Fatal("RPL_INVITING (341) must write 'Invited <nick> to <channel>' to the status buffer")
	}
}

func TestHandleInviting_RPL341TooFewParamsIsIgnored(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// Fewer than 3 params — must not panic.
	msg := ircmsg.MakeMessage(nil, "server", "341", "me", "bob")
	c.handleInviting(msg) // must not panic or write anything

	msgs, err := c.storage.GetMessages(c.networkID, nil, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no status rows, got %d", len(msgs))
	}
}

func TestHandleUserOnChannel_443WritesError(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// 443 ERR_USERONCHANNEL: "<me> <nick> <channel> :is already on channel"
	msg := ircmsg.MakeMessage(nil, "server", "443", "me", "bob", "#chan", "is already on channel")
	c.handleUserOnChannel(msg)

	if !statusBufferHas(t, c, "status", "bob is already on #chan") {
		t.Fatal("ERR_USERONCHANNEL (443) must write '<nick> is already on <channel>' to the status buffer")
	}
}

func TestHandleUserOnChannel_443TooFewParamsIsIgnored(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// Fewer than 3 params — must not panic.
	msg := ircmsg.MakeMessage(nil, "server", "443", "me", "bob")
	c.handleUserOnChannel(msg) // must not panic or write anything

	msgs, err := c.storage.GetMessages(c.networkID, nil, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no status rows, got %d", len(msgs))
	}
}
