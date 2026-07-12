package cascade

import (
	"strings"
	"time"
)

// TextEvent is delivered to OnText handlers. The host constructs it via
// NewTextEvent and wires Reply to a sink; the script only reads fields and replies.
type TextEvent struct {
	Nick    string
	Channel string // "#chan" for a channel message; the bot's own nick for a DM
	Message string

	// Context added by the host (zero values when unavailable).
	Self    string // the bot's own nick on this network
	Account string // sender's logged-in account; "" if not logged in
	Network string // configured network name this message arrived on
	MsgID   string // IRCv3 msgid; "" if the server sent none
	Time    Time   // message timestamp (server-time aware)
	Action  bool   // true if this was a CTCP ACTION (/me)

	replyFn     func(string)
	direct      bool
	directKnown bool
}

// NewTextEvent is the host-side constructor. Scripts never call it.
func NewTextEvent(nick, channel, message string, reply func(string)) TextEvent {
	return TextEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply}
}

// NewTextEventWithDirect is the host-side constructor with authoritative target classification.
func NewTextEventWithDirect(nick, channel, message string, direct bool, reply func(string)) TextEvent {
	return TextEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply, direct: direct, directKnown: true}
}

// IsDM reports whether this message was a direct message (Channel is not a channel name).
func (e TextEvent) IsDM() bool {
	if e.directKnown {
		return e.direct
	}
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

// IsHighlight reports whether the bot's own nick (Self) appears in Message as a
// whole word, case-insensitive. Returns false when Self is empty.
func (e TextEvent) IsHighlight() bool {
	if e.Self == "" {
		return false
	}
	return containsWord(strings.ToLower(e.Message), strings.ToLower(e.Self))
}

// containsWord reports whether word appears in s bounded by non-alphanumerics
// (or string ends). Both args must already be lowercased.
func containsWord(s, word string) bool {
	if word == "" {
		return false
	}
	from := 0
	for {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return false
		}
		i += from
		leftOK := i == 0 || !isWordByte(s[i-1])
		end := i + len(word)
		rightOK := end == len(s) || !isWordByte(s[end])
		if leftOK && rightOK {
			return true
		}
		from = i + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// NoticeEvent is delivered to OnNotice handlers (an inbound NOTICE).
type NoticeEvent struct {
	Nick    string
	Channel string
	Message string

	// Context added by the host (zero values when unavailable).
	Self    string // the bot's own nick on this network
	Account string // sender's logged-in account; "" if not logged in
	Network string // configured network name this message arrived on
	MsgID   string // IRCv3 msgid; "" if the server sent none
	Time    Time   // message timestamp (server-time aware)

	replyFn     func(string)
	direct      bool
	directKnown bool
}

// NewNoticeEvent is the host-side constructor. Scripts never call it.
func NewNoticeEvent(nick, channel, message string, reply func(string)) NoticeEvent {
	return NoticeEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply}
}

// NewNoticeEventWithDirect is the host-side constructor with authoritative target classification.
func NewNoticeEventWithDirect(nick, channel, message string, direct bool, reply func(string)) NoticeEvent {
	return NoticeEvent{Nick: nick, Channel: channel, Message: message, replyFn: reply, direct: direct, directKnown: true}
}

// IsDM reports whether this notice was a direct message (Channel is not a channel name).
func (e NoticeEvent) IsDM() bool {
	if e.directKnown {
		return e.direct
	}
	return e.Channel == "" || (e.Channel[0] != '#' && e.Channel[0] != '&')
}

// Reply sends a reply to the notice's origin. No-op if no sink is wired.
func (e NoticeEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}

// IsHighlight reports whether the bot's own nick (Self) appears in Message as a
// whole word, case-insensitive. Returns false when Self is empty.
func (e NoticeEvent) IsHighlight() bool {
	if e.Self == "" {
		return false
	}
	return containsWord(strings.ToLower(e.Message), strings.ToLower(e.Self))
}

// JoinEvent is delivered to OnJoin handlers when a user joins a channel.
type JoinEvent struct {
	Nick     string
	Channel  string
	Self     string
	Account  string
	Network  string
	Host     string
	Realname string
	Time     Time

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
	Nick     string
	Channel  string
	Reason   string
	Self     string
	Account  string
	Network  string
	Host     string
	Realname string
	Time     Time

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

// QuitEvent is delivered to OnQuit when a user leaves the network.
type QuitEvent struct {
	Nick     string
	Reason   string
	Self     string
	Account  string
	Network  string
	Host     string
	Realname string
	Time     Time
}

// NewQuitEvent is the host-side constructor.
func NewQuitEvent(nick, reason string) QuitEvent { return QuitEvent{Nick: nick, Reason: reason} }

// KickEvent is delivered to OnKick when a user is kicked from a channel.
type KickEvent struct {
	Nick    string
	By      string
	Channel string
	Reason  string
	Self    string
	Account string
	Network string
	Time    Time

	replyFn func(string)
}

// NewKickEvent is the host-side constructor.
func NewKickEvent(nick, by, channel, reason string, reply func(string)) KickEvent {
	return KickEvent{Nick: nick, By: by, Channel: channel, Reason: reason, replyFn: reply}
}

// Reply sends a message to the channel where the kick occurred.
func (e KickEvent) Reply(message string) {
	if e.replyFn != nil {
		e.replyFn(message)
	}
}

// NickEvent is delivered to OnNick when a user changes nickname.
type NickEvent struct {
	OldNick string
	NewNick string
	Self    string
	Account string
	Network string
	Time    Time
}

// NewNickEvent is the host-side constructor.
func NewNickEvent(oldNick, newNick string) NickEvent {
	return NickEvent{OldNick: oldNick, NewNick: newNick}
}

// Time is a script-safe timestamp. Scripts can't import "time", so the cascade
// package (compiled host code) owns formatting and comparison.
type Time struct {
	unix int64
}

// NewTime constructs a Time from Unix seconds. The host-side constructor.
func NewTime(unix int64) Time { return Time{unix: unix} }

// Unix returns the timestamp in Unix seconds.
func (t Time) Unix() int64 { return t.unix }

// IsZero reports whether the timestamp is unset (zero Unix seconds).
func (t Time) IsZero() bool { return t.unix == 0 }

// Before reports whether t is before o.
func (t Time) Before(o Time) bool { return t.unix < o.unix }

// After reports whether t is after o.
func (t Time) After(o Time) bool { return t.unix > o.unix }

// Clock returns the local-time "HH:MM" (24-hour) of the timestamp.
func (t Time) Clock() string { return time.Unix(t.unix, 0).Format("15:04") }
