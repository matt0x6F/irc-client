package cascade

import "strings"

// TextEvent is delivered to OnText handlers. The host constructs it via
// NewTextEvent and wires Reply to a sink; the script only reads fields and replies.
type TextEvent struct {
	Nick    string
	Channel string // "#chan" for a channel message; the bot's own nick for a DM
	Message string

	replyFn func(string)
}

// NewTextEvent is the host-side constructor. Scripts never call it.
func NewTextEvent(nick, channel, message string, reply func(string)) TextEvent {
	return TextEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply}
}

// IsDM reports whether this message was a direct message (Channel is not a channel name).
func (e TextEvent) IsDM() bool {
	return e.Channel == "" || (e.Channel[0] != '#' && e.Channel[0] != '&')
}

// HasPrefix reports whether the message starts with s.
func (e TextEvent) HasPrefix(s string) bool { return strings.HasPrefix(e.Message, s) }

// Arg returns the n-th whitespace-separated field (1-based), or "" if out of range.
func (e TextEvent) Arg(n int) string {
	f := strings.Fields(e.Message)
	if n < 1 || n > len(f) {
		return ""
	}
	return f[n-1]
}

// Reply sends a reply to the message's origin. No-op if no sink is wired.
func (e TextEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}
