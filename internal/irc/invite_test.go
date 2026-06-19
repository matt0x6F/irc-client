package irc

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/storage"
)

// statusHasInvite reports whether the status buffer holds an "invite" line whose
// text matches want.
func statusHasInvite(msgs []storage.Message, want string) bool {
	for _, m := range msgs {
		if m.MessageType == "invite" && m.Message == want {
			return true
		}
	}
	return false
}

func TestInviteToSelfWritesStatusLine(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	// The test network's nick is matt0x6f; CurrentNick() falls back to it.
	c.handleInvite(parse(t, ":alice!a@h INVITE matt0x6f #chan"))

	msgs, err := c.storage.GetMessages(c.networkID, nil, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if !statusHasInvite(msgs, "alice invited you to #chan") {
		t.Fatalf("expected self-invite status line; got %+v", msgs)
	}
}

func TestInviteOfAnotherUser(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.handleInvite(parse(t, ":alice!a@h INVITE bob #chan"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	if !statusHasInvite(msgs, "alice invited bob to #chan") {
		t.Fatalf("expected other-invite status line; got %+v", msgs)
	}
}

func TestInviteIgnoredWithoutParams(t *testing.T) {
	c, _ := newUserMetaTestClient(t)
	c.handleInvite(parse(t, ":alice!a@h INVITE"))

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	for _, m := range msgs {
		if m.MessageType == "invite" {
			t.Fatalf("INVITE with no params wrote a line: %+v", m)
		}
	}
}
