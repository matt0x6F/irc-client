package irc

// This file holds small, pure helpers that implement parts of the IRC protocol
// defined at https://defs.ircdocs.horse/ — CASEMAPPING-aware case folding,
// CHANTYPES-aware channel detection, and formatting of generic server numerics.
// They are kept free of client state so they can be unit-tested directly.

import (
	"fmt"
	"strings"
	"time"
)

// defaultChanTypes is the conventional channel-prefix set used before the server
// advertises CHANTYPES in RPL_ISUPPORT (and when it never does).
const defaultChanTypes = "#&"

// casefold folds s for case-insensitive nick/channel comparison using the
// server's advertised CASEMAPPING (RPL_ISUPPORT). IRC predates Unicode, so the
// fold is byte-oriented:
//
//   - "ascii": only A–Z fold to a–z.
//   - "rfc1459": as ascii, plus the characters {}|^ are the lowercase of []\~
//     (because those were "the same letter" on the terminals RFC 1459 targeted).
//   - "rfc1459-strict": as rfc1459 but WITHOUT the ~ ↔ ^ pairing.
//   - absent/empty: rfc1459, the protocol default when the server says nothing.
//   - anything else (precis/utf8/…): conservative ascii fold.
//
// Non-ASCII bytes are passed through unchanged.
func casefold(mapping, s string) string {
	var foldBrackets, foldTilde bool
	switch strings.ToLower(mapping) {
	case "ascii":
		// only A–Z
	case "rfc1459-strict", "strict-rfc1459":
		foldBrackets = true
	case "rfc1459", "":
		foldBrackets, foldTilde = true, true
	default:
		// Unknown mapping (precis/utf8/…): conservative ASCII fold.
	}

	b := []byte(s)
	for i := range len(b) {
		switch c := b[i]; {
		case c >= 'A' && c <= 'Z':
			b[i] = c + 32
		case foldBrackets && c == '[':
			b[i] = '{'
		case foldBrackets && c == ']':
			b[i] = '}'
		case foldBrackets && c == '\\':
			b[i] = '|'
		case foldTilde && c == '~':
			b[i] = '^'
		}
	}
	return string(b)
}

// channelNameMatches reports whether name is a channel (vs. a nick/PM target)
// according to the server's CHANTYPES (RPL_ISUPPORT). chantypes is the set of
// channel-prefix characters (e.g. "#&"); empty falls back to the conventional
// "#&".
func channelNameMatches(name, chantypes string) bool {
	if chantypes == "" {
		chantypes = defaultChanTypes
	}
	return len(name) > 0 && strings.IndexByte(chantypes, name[0]) >= 0
}

// topicWhoTimeLayout is the human-readable timestamp shown for RPL_TOPICWHOTIME
// (333). Kept in one place so the format is consistent and testable.
const topicWhoTimeLayout = "Jan 2, 2006 15:04"

// formatTopicWhoTime renders RPL_TOPICWHOTIME (333) — who set a channel's topic
// and when — into a channel system line. setter may be empty (some servers omit
// it), in which case only the time is shown.
func formatTopicWhoTime(setter string, when time.Time) string {
	if setter == "" {
		return "Topic set on " + when.Format(topicWhoTimeLayout)
	}
	return fmt.Sprintf("Topic set by %s on %s", setter, when.Format(topicWhoTimeLayout))
}

// formatServerNumeric renders a generic server numeric reply (e.g. ERR_NOSUCHNICK)
// into a human-readable status line. params are the raw message params: params[0]
// is our own nick, the trailing param is the human-readable description, and any
// params between are the subject(s) the reply is about (a nick, channel, command,
// …). Returns "" when there is nothing to show.
func formatServerNumeric(params []string) string {
	if len(params) < 2 {
		return ""
	}
	description := params[len(params)-1]
	subjects := params[1 : len(params)-1]
	if len(subjects) == 0 {
		return description
	}
	return strings.Join(subjects, " ") + ": " + description
}
