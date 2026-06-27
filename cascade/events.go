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

// NoticeEvent is delivered to OnNotice handlers (an inbound NOTICE).
type NoticeEvent struct {
	Nick    string
	Channel string
	Message string

	replyFn func(string)
}

// NewNoticeEvent is the host-side constructor. Scripts never call it.
func NewNoticeEvent(nick, channel, message string, reply func(string)) NoticeEvent {
	return NoticeEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply}
}

// IsDM reports whether this notice was a direct message (Channel is not a channel name).
func (e NoticeEvent) IsDM() bool {
	return e.Channel == "" || (e.Channel[0] != '#' && e.Channel[0] != '&')
}

// Reply sends a reply to the notice's origin. No-op if no sink is wired.
func (e NoticeEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}

// JoinEvent is delivered to OnJoin handlers when a user joins a channel.
type JoinEvent struct {
	Nick    string
	Channel string

	replyFn func(string)
}

// NewJoinEvent is the host-side constructor. Scripts never call it.
func NewJoinEvent(nick, channel string, reply func(string)) JoinEvent {
	return JoinEvent{Nick: nick, Channel: channel, replyFn: reply}
}

// Reply sends a reply to the channel. No-op if no sink is wired.
func (e JoinEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}

// PartEvent is delivered to OnPart handlers when a user leaves a channel.
type PartEvent struct {
	Nick    string
	Channel string
	Reason  string

	replyFn func(string)
}

// NewPartEvent is the host-side constructor. Scripts never call it.
func NewPartEvent(nick, channel, reason string, reply func(string)) PartEvent {
	return PartEvent{Nick: nick, Channel: channel, Reason: reason, replyFn: reply}
}

// Reply sends a reply to the channel. No-op if no sink is wired.
func (e PartEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}
