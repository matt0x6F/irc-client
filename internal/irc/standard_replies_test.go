package irc

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/storage"
)

func hasStatusLine(msgs []storage.Message, mtype, text string) bool {
	for _, m := range msgs {
		if m.MessageType == mtype && m.Message == text {
			return true
		}
	}
	return false
}

func TestStandardReplyFailMapsToError(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleStandardReply(parse(t, "FAIL JOIN NEED_REGISTRATION #chan :You must be registered"), "error")

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	if !hasStatusLine(msgs, "error", "FAIL JOIN (NEED_REGISTRATION): You must be registered") {
		t.Fatalf("missing FAIL error line; got %+v", msgs)
	}
}

func TestStandardReplyWarnAndNote(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleStandardReply(parse(t, "WARN * ACCOUNT_REQUIRED :Heads up"), "warning")
	c.handleStandardReply(parse(t, "NOTE NICK :name accepted"), "status")

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	if !hasStatusLine(msgs, "warning", "WARN * (ACCOUNT_REQUIRED): Heads up") {
		t.Fatalf("missing WARN warning line; got %+v", msgs)
	}
	// NOTE with only command + description (no code) omits the parenthetical.
	if !hasStatusLine(msgs, "status", "NOTE NICK: name accepted") {
		t.Fatalf("missing NOTE status line; got %+v", msgs)
	}
}

func TestStandardReplyIgnoredWhenTooShort(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.handleStandardReply(parse(t, "FAIL"), "error")

	msgs, _ := c.storage.GetMessages(c.networkID, nil, 10)
	for _, m := range msgs {
		if m.MessageType == "error" {
			t.Fatalf("malformed FAIL wrote a line: %+v", m)
		}
	}
}
