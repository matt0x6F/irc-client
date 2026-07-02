package ircevent

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/ergochat/irc-go/ircutils"
)

// fakeMech is a minimal SASLMechanism used to exercise the pluggable hook
// without a live connection.
type fakeMech struct {
	name  string
	resps [][]byte
	err   error
	calls int
}

func (m *fakeMech) Name() string { return m.name }

func (m *fakeMech) Respond(challenge []byte) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	r := m.resps[m.calls]
	m.calls++
	return r, nil
}

// TestSASLMechanismNameValidation exercises the same precondition Connect()
// enforces (SASLMech must equal SASLMechanism.Name()) without needing a
// socket: it is a pure function of two string fields.
func TestSASLMechanismNameValidation(t *testing.T) {
	c := &Connection{Server: "x:1", Nick: "n", SASLMech: "SCRAM-SHA-256",
		SASLMechanism: &fakeMech{name: "SCRAM-SHA-256"}, UseSASL: true,
		KeepAlive: 240, Timeout: 60}
	if c.SASLMechanism.Name() != c.SASLMech {
		t.Fatal("precondition: names should match")
	}

	c.SASLMech = "PLAIN"
	if c.SASLMechanism.Name() == c.SASLMech {
		t.Fatal("expected mismatch after changing SASLMech")
	}
}

// TestSASLMechanismRespondErrorSurfaced confirms that an error returned by
// Respond is exactly the error the mechanism produced (this is what
// setupSASLCallbacks forwards into saslResult.Err when SASLMechanism != nil).
func TestSASLMechanismRespondErrorSurfaced(t *testing.T) {
	boom := errors.New("boom")
	m := &fakeMech{name: "X", err: boom}
	resp, err := m.Respond([]byte("+"))
	if err == nil {
		t.Fatal("expected error from Respond")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected the exact sentinel error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response alongside an error, got %v", resp)
	}
}

// TestSASLBufferReassemblyMultiChunk drives *ircutils.SASLBuffer directly the
// way setupSASLCallbacks' AUTHENTICATE handler does: feed it successive
// AUTHENTICATE parameter chunks and confirm it reports done only once the
// full (base64-decoded) challenge has been reassembled.
func TestSASLBufferReassemblyMultiChunk(t *testing.T) {
	// Build a challenge long enough that its base64 encoding exceeds the
	// 400-byte AUTHENTICATE chunk limit, forcing EncodeSASLResponse to split
	// it into multiple chunks. 400 bytes of base64 decodes to 300 raw bytes,
	// so 500 raw bytes guarantees at least two chunks.
	challenge := bytes.Repeat([]byte("challenge-data-"), 32) // 512 bytes
	chunks := ircutils.EncodeSASLResponse(challenge)
	if len(chunks) < 2 {
		t.Fatalf("test setup: expected multiple chunks, got %d", len(chunks))
	}

	buf := ircutils.NewSASLBuffer(0)
	var (
		done      bool
		reasm     []byte
		err       error
		doneIndex = -1
	)
	for i, chunk := range chunks {
		done, reasm, err = buf.Add(chunk)
		if err != nil {
			t.Fatalf("chunk %d: unexpected error: %v", i, err)
		}
		if done {
			doneIndex = i
			break
		}
	}

	if doneIndex != len(chunks)-1 {
		t.Fatalf("expected done only on the final chunk (index %d), got done at index %d", len(chunks)-1, doneIndex)
	}
	if !bytes.Equal(reasm, challenge) {
		t.Fatalf("reassembled challenge mismatch:\n got  %x\n want %x", reasm, challenge)
	}
}

// TestSASLBufferReassemblySingleChunkPlusSign confirms the common case: a
// short challenge fits in one AUTHENTICATE chunk, so Add reports done
// immediately without needing a trailing "+".
func TestSASLBufferReassemblySingleChunkPlusSign(t *testing.T) {
	challenge := []byte("short-challenge")
	chunks := ircutils.EncodeSASLResponse(challenge)
	if len(chunks) != 1 {
		t.Fatalf("test setup: expected exactly one chunk for a short challenge, got %d", len(chunks))
	}

	buf := ircutils.NewSASLBuffer(0)
	done, reasm, err := buf.Add(chunks[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Fatal("expected done after the only chunk")
	}
	if !bytes.Equal(reasm, challenge) {
		t.Fatalf("reassembled challenge mismatch: got %q want %q", reasm, challenge)
	}
}

// TestEncodeSASLResponseChunksLongResponseWithTrailingPlus verifies
// EncodeSASLResponse's documented behavior: a response whose base64 encoding
// is an exact multiple of 400 bytes must be followed by a trailing "+" chunk
// to signal end-of-response, and every non-final chunk must be exactly 400
// bytes of base64.
func TestEncodeSASLResponseChunksLongResponseWithTrailingPlus(t *testing.T) {
	// 300 raw bytes -> exactly 400 base64 bytes (4/3 ratio), so this hits the
	// documented edge case requiring a trailing "+".
	raw := bytes.Repeat([]byte{0x41}, 300)
	if got := base64.StdEncoding.EncodedLen(len(raw)); got != 400 {
		t.Fatalf("test setup: expected 300 raw bytes to encode to 400 base64 bytes, got %d", got)
	}

	chunks := ircutils.EncodeSASLResponse(raw)
	if len(chunks) != 2 {
		t.Fatalf("expected exactly 2 chunks (400-byte payload + trailing '+'), got %d: %v", len(chunks), chunks)
	}
	if len(chunks[0]) != 400 {
		t.Fatalf("expected first chunk to be exactly 400 bytes, got %d", len(chunks[0]))
	}
	if chunks[1] != "+" {
		t.Fatalf("expected trailing '+' chunk, got %q", chunks[1])
	}

	// Round-trip: feed the emitted chunks back into a SASLBuffer and confirm
	// we recover the original >400-byte response.
	buf := ircutils.NewSASLBuffer(0)
	var (
		done bool
		out  []byte
		err  error
	)
	for _, c := range chunks {
		done, out, err = buf.Add(c)
		if err != nil {
			t.Fatalf("unexpected error feeding chunk %q: %v", c, err)
		}
	}
	if !done {
		t.Fatal("expected done after feeding all chunks including trailing '+'")
	}
	if !bytes.Equal(out, raw) {
		t.Fatal("round-tripped response does not match original")
	}
}

// TestSASLMechanismFieldsExist is a compile-time-ish smoke test: it exercises
// the new Connection fields (SASLMechanism, saslBuffer) directly to lock in
// their types. If these fields are renamed or removed this test fails to
// compile, which is the point.
func TestSASLMechanismFieldsExist(t *testing.T) {
	var mech SASLMechanism = &fakeMech{name: "PLAIN", resps: [][]byte{[]byte("resp")}}
	c := &Connection{SASLMechanism: mech}
	if c.SASLMechanism == nil {
		t.Fatal("expected SASLMechanism field to be settable")
	}
	if name := c.SASLMechanism.Name(); name != "PLAIN" {
		t.Fatalf("unexpected name: %q", name)
	}
	resp, err := c.SASLMechanism.Respond([]byte("chal"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp) != "resp" {
		t.Fatalf("unexpected response: %q", resp)
	}

	// saslBuffer is unexported; verify it exists and defaults to nil, then
	// that it can hold a *ircutils.SASLBuffer once assigned (mirrors what
	// the AUTHENTICATE callback does on first inbound chunk).
	if c.saslBuffer != nil {
		t.Fatal("expected saslBuffer to be nil by default")
	}
	c.saslBuffer = ircutils.NewSASLBuffer(0)
	if c.saslBuffer == nil {
		t.Fatal("expected saslBuffer to be settable to a *ircutils.SASLBuffer")
	}
}

// ensure fakeMech satisfies the interface at compile time
var _ SASLMechanism = (*fakeMech)(nil)
