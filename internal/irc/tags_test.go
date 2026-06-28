package irc

import (
	"testing"

	"github.com/ergochat/irc-go/ircmsg"
)

func msgWithTags(tags map[string]string) ircmsg.Message {
	m := ircmsg.MakeMessage(tags, "nick!u@h", "PRIVMSG", "#chan", "hi")
	return m
}

func TestGetReplyTagAcceptsBothPrefixes(t *testing.T) {
	c := &IRCClient{}
	if got := c.getReplyTag(msgWithTags(map[string]string{"+draft/reply": "abc"})); got != "abc" {
		t.Fatalf("draft form: got %q", got)
	}
	if got := c.getReplyTag(msgWithTags(map[string]string{"+reply": "xyz"})); got != "xyz" {
		t.Fatalf("bare form: got %q", got)
	}
	if got := c.getReplyTag(msgWithTags(nil)); got != "" {
		t.Fatalf("absent: got %q", got)
	}
}

func TestGetChannelContextValidatesChannelName(t *testing.T) {
	c := &IRCClient{} // chanTypes defaults handled in getter
	if got := c.getChannelContext(msgWithTags(map[string]string{"+draft/channel-context": "#dev"})); got != "#dev" {
		t.Fatalf("valid channel: got %q", got)
	}
	// Not a channel name -> ignored per spec.
	if got := c.getChannelContext(msgWithTags(map[string]string{"+draft/channel-context": "notachannel"})); got != "" {
		t.Fatalf("invalid value should be ignored: got %q", got)
	}
}
