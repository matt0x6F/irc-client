package irc

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitOutboundMessage_ShortMessageUnchanged(t *testing.T) {
	got := splitOutboundMessage("hello world", 400)
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("splitOutboundMessage = %q, want [\"hello world\"]", got)
	}
}

// A multiline paste becomes one message per non-empty line — each line is its
// own PRIVMSG on the wire, matching what other clients do.
func TestSplitOutboundMessage_Newlines(t *testing.T) {
	got := splitOutboundMessage("first\r\nsecond\n\nthird", 400)
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("splitOutboundMessage = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Long prose splits at word boundaries: every chunk fits the budget, no chunk
// has leading/trailing spaces, and no words are lost or mangled.
func TestSplitOutboundMessage_WordBoundaries(t *testing.T) {
	message := strings.TrimSpace(strings.Repeat("lorem ipsum dolor sit amet ", 40))
	const maxBytes = 100

	chunks := splitOutboundMessage(message, maxBytes)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if len(chunk) > maxBytes {
			t.Errorf("chunk %d is %d bytes, over budget %d", i, len(chunk), maxBytes)
		}
		if chunk != strings.TrimSpace(chunk) {
			t.Errorf("chunk %d has surrounding spaces: %q", i, chunk)
		}
		if chunk == "" {
			t.Errorf("chunk %d is empty", i)
		}
	}
	joined := strings.Join(chunks, " ")
	if joined != message {
		t.Errorf("rejoined chunks != original\n got: %q\nwant: %q", joined, message)
	}
}

// An unbroken token (no spaces) hard-splits exactly at the byte budget.
func TestSplitOutboundMessage_HardSplit(t *testing.T) {
	message := strings.Repeat("a", 250)
	chunks := splitOutboundMessage(message, 100)
	want := []string{strings.Repeat("a", 100), strings.Repeat("a", 100), strings.Repeat("a", 50)}
	if len(chunks) != len(want) {
		t.Fatalf("got %d chunks, want %d: %q", len(chunks), len(want), chunks)
	}
	for i := range want {
		if chunks[i] != want[i] {
			t.Errorf("chunk %d = %q, want %q", i, chunks[i], want[i])
		}
	}
}

// Multibyte input never gets split mid-rune: every chunk is valid UTF-8 and the
// concatenation is byte-identical to the input.
func TestSplitOutboundMessage_UTF8Boundaries(t *testing.T) {
	cases := map[string]string{
		"two-byte runes":   strings.Repeat("héllo", 100),
		"three-byte runes": strings.Repeat("日本語テスト", 60),
		"four-byte runes":  strings.Repeat("😀🎉", 80),
		"mixed widths":     strings.Repeat("a€b😀c", 70),
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			for _, maxBytes := range []int{7, 100, 401} {
				chunks := splitOutboundMessage(message, maxBytes)
				var rebuilt strings.Builder
				for i, chunk := range chunks {
					if len(chunk) > maxBytes {
						t.Errorf("maxBytes=%d: chunk %d is %d bytes", maxBytes, i, len(chunk))
					}
					if !utf8.ValidString(chunk) {
						t.Errorf("maxBytes=%d: chunk %d is invalid UTF-8: %q", maxBytes, i, chunk)
					}
					rebuilt.WriteString(chunk)
				}
				if rebuilt.String() != message {
					t.Errorf("maxBytes=%d: rebuilt message differs from input", maxBytes)
				}
			}
		})
	}
}

// A pathological budget below the max rune width still terminates and still
// produces valid UTF-8 (the splitter enforces a floor of one whole rune).
func TestSplitOutboundMessage_TinyBudget(t *testing.T) {
	chunks := splitOutboundMessage("😀😀😀", 1)
	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}
	var rebuilt strings.Builder
	for i, chunk := range chunks {
		if !utf8.ValidString(chunk) {
			t.Errorf("chunk %d is invalid UTF-8: %q", i, chunk)
		}
		rebuilt.WriteString(chunk)
	}
	if rebuilt.String() != "😀😀😀" {
		t.Errorf("rebuilt = %q, want the original emoji", rebuilt.String())
	}
}

func TestSplitOutboundMessage_EmptyAndWhitespaceOnly(t *testing.T) {
	if got := splitOutboundMessage("", 400); len(got) != 0 {
		t.Errorf("empty message: got %q, want no chunks", got)
	}
	if got := splitOutboundMessage("\n\n\r\n", 400); len(got) != 0 {
		t.Errorf("newlines only: got %q, want no chunks", got)
	}
}

// maxMessageChunk budgets 512 bytes minus the relayed-line overhead
// ":nick!user@host PRIVMSG <target> :...\r\n". Before we've learned our own
// user@host it uses a conservative worst-case estimate.
func TestMaxMessageChunk_FallbackEstimate(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	// nick "matt0x6f" (8 bytes) via preferredNick fallback; no self user-meta.
	// overhead = len(nick) + fallbackUserHostLen + len(target) + 15
	got := c.maxMessageChunk("#go")
	want := 512 - (8 + fallbackUserHostLen + 3 + 15)
	if got != want {
		t.Errorf("maxMessageChunk(#go) = %d, want %d", got, want)
	}
}

// Once our own user@host is known (WHOX / userhost-in-names / chghost), the
// budget uses the real length instead of the worst-case estimate.
func TestMaxMessageChunk_UsesKnownSelfHost(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	host := "~u@example.org" // 14 bytes
	c.applyUserMeta("matt0x6f", func(m *UserMeta) { m.Host = host })

	got := c.maxMessageChunk("#go")
	want := 512 - (8 + len(host) + 3 + 15)
	if got != want {
		t.Errorf("maxMessageChunk(#go) = %d, want %d", got, want)
	}
}

// LINELEN raises the whole budget when the server advertises it.
func TestMaxMessageChunk_HonorsLineLen(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	c.applyISUPPORTToken("LINELEN=1024")
	got := c.maxMessageChunk("#go")
	want := 1024 - (8 + fallbackUserHostLen + 3 + 15)
	if got != want {
		t.Errorf("maxMessageChunk(#go) = %d, want %d", got, want)
	}
}

// The budget never collapses below the floor, no matter how absurd the target.
func TestMaxMessageChunk_Floor(t *testing.T) {
	c, _ := newUserMetaTestClient(t)

	if got := c.maxMessageChunk(strings.Repeat("#", 600)); got != minMessageChunk {
		t.Errorf("maxMessageChunk(long target) = %d, want floor %d", got, minMessageChunk)
	}
}
