package irc

import (
	"strings"
	"unicode/utf8"
)

// Outbound message splitting (RFC 1459 §2.3): an IRC line is capped at 512
// bytes including the trailing CRLF, and the cap applies to the line as
// *relayed* to other clients — i.e. with our ":nick!user@host " source prefix
// prepended by the server. The underlying library refuses to send an
// over-budget line (ErrorBodyTooLong), so anything longer than one line must
// be split client-side into multiple PRIVMSGs. Splitting is byte-based but
// must never land inside a UTF-8 sequence, or the receiving side renders
// mojibake at the seam.

const (
	// defaultLineLen is the RFC 1459 line cap (bytes, including CRLF), used
	// unless the server advertises a larger LINELEN.
	defaultLineLen = 512

	// fallbackUserHostLen is the worst-case "user@host" length assumed before
	// we've learned our own hostmask: 10-byte username plus a '~' ident prefix,
	// '@', and a 63-byte hostname (the DNS label cap servers enforce; cloaks
	// stay within it). Overestimating only costs a slightly earlier split.
	fallbackUserHostLen = 11 + 1 + 63

	// minMessageChunk floors the per-chunk budget so a pathological target or
	// tiny LINELEN can never drive the budget to zero and stall the splitter.
	minMessageChunk = 64
)

// splitOutboundMessage prepares a user-typed message for the wire: each
// newline becomes its own message (multiline paste), and each line longer
// than maxBytes is split into chunks of at most maxBytes bytes, preferring a
// space boundary and never cutting a UTF-8 sequence. Empty lines are dropped.
func splitOutboundMessage(message string, maxBytes int) []string {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.ReplaceAll(message, "\r", "\n")

	var chunks []string
	for line := range strings.SplitSeq(message, "\n") {
		if line == "" {
			continue
		}
		chunks = append(chunks, splitLine(line, maxBytes)...)
	}
	return chunks
}

// splitLine splits a single line into chunks of at most maxBytes bytes. A
// chunk breaks at the last space that fits when there is one (the space is
// consumed, not emitted); otherwise it hard-splits at the last UTF-8 rune
// boundary within the budget. maxBytes is floored at one whole rune so the
// loop always makes progress.
func splitLine(line string, maxBytes int) []string {
	if maxBytes < utf8.UTFMax {
		maxBytes = utf8.UTFMax
	}

	var chunks []string
	for len(line) > maxBytes {
		cut := lastRuneBoundary(line, maxBytes)
		if sp := strings.LastIndexByte(line[:cut], ' '); sp > 0 {
			chunks = append(chunks, strings.TrimRight(line[:sp], " "))
			line = strings.TrimLeft(line[sp+1:], " ")
		} else {
			chunks = append(chunks, line[:cut])
			line = line[cut:]
		}
	}
	if line != "" {
		chunks = append(chunks, line)
	}
	return chunks
}

// lastRuneBoundary returns the largest i <= max such that s[:i] ends on a
// UTF-8 rune boundary. Assumes len(s) > max. For invalid UTF-8 (no boundary
// found within the last rune's width) it falls back to a hard byte cut.
func lastRuneBoundary(s string, max int) int {
	for i := max; i > 0 && max-i < utf8.UTFMax; i-- {
		if utf8.RuneStart(s[i]) {
			return i
		}
	}
	return max
}

// maxMessageChunk returns the per-message byte budget for PRIVMSGs to target:
// the server's line cap (LINELEN, default 512) minus the overhead of the line
// as relayed to other clients — ":<nick>!<user@host> PRIVMSG <target> :" plus
// CRLF. Our own user@host is used when the roster has taught it to us
// (userhost-in-names / WHOX / chghost); otherwise a conservative worst case.
func (c *IRCClient) maxMessageChunk(target string) int {
	lineLen := defaultLineLen
	c.mu.RLock()
	if c.serverCapabilities != nil && c.serverCapabilities.LineLen > 0 {
		lineLen = c.serverCapabilities.LineLen
	}
	c.mu.RUnlock()

	nick := c.CurrentNick()
	userHostLen := fallbackUserHostLen
	if meta, ok := c.UserMetaFor(nick); ok && meta.Host != "" {
		userHostLen = len(meta.Host)
	}

	// ":" + nick + "!" + user@host + " " + "PRIVMSG " + target + " :" + CRLF
	overhead := 1 + len(nick) + 1 + userHostLen + 1 + len("PRIVMSG ") + len(target) + len(" :") + 2
	budget := lineLen - overhead
	if budget < minMessageChunk {
		return minMessageChunk
	}
	return budget
}
