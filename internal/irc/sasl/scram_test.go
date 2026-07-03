package sasl

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// drive one full exchange with a deterministic client nonce
func newTestScram(t *testing.T) *Scram {
	m, err := NewScram("SCRAM-SHA-256", "user", "pencil")
	if err != nil {
		t.Fatal(err)
	}
	s := m.(*Scram)
	s.nonce = "rOprNGfwEbeRWgbNEkqO" // fixed for reproducibility
	return s
}

func TestScramClientFirst(t *testing.T) {
	s := newTestScram(t)
	got, err := s.Respond([]byte("+"))
	if err != nil {
		t.Fatal(err)
	}
	want := "n,,n=user,r=rOprNGfwEbeRWgbNEkqO"
	if string(got) != want {
		t.Fatalf("client-first got %q want %q", got, want)
	}
}

func TestScramClientFinalHasProof(t *testing.T) {
	s := newTestScram(t)
	if _, err := s.Respond([]byte("+")); err != nil {
		t.Fatal(err)
	}
	salt := base64.StdEncoding.EncodeToString([]byte("saltSALTsalt"))
	serverFirst := "r=rOprNGfwEbeRWgbNEkqO2222,s=" + salt + ",i=4096"
	got, err := s.Respond([]byte(serverFirst))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "c=biws,r=rOprNGfwEbeRWgbNEkqO2222,p=") {
		t.Fatalf("client-final shape wrong: %q", got)
	}
}

func TestScramRejectsBadServerNonce(t *testing.T) {
	s := newTestScram(t)
	if _, err := s.Respond([]byte("+")); err != nil {
		t.Fatal(err)
	}
	_, err := s.Respond([]byte("r=WRONGNONCE,s=" + base64.StdEncoding.EncodeToString([]byte("x")) + ",i=4096"))
	if err == nil {
		t.Fatal("expected server-nonce mismatch error")
	}
}

func TestScramVerifiesServerSignature(t *testing.T) {
	// Round-trip against our own server-side computation: derive the expected
	// server signature the way a compliant server would, feed it back, expect
	// an empty (success) response; then corrupt it and expect an error.
	s := newTestScram(t)
	s.Respond([]byte("+"))
	salt := base64.StdEncoding.EncodeToString([]byte("saltSALTsalt"))
	serverFirst := "r=rOprNGfwEbeRWgbNEkqO2222,s=" + salt + ",i=4096"
	s.Respond([]byte(serverFirst))
	good := "v=" + base64.StdEncoding.EncodeToString(serverSignatureForTest(s))
	got, err := s.Respond([]byte(good))
	if err != nil || len(got) != 0 {
		t.Fatalf("valid signature should succeed with empty resp: %v %q", err, got)
	}

	s2 := newTestScram(t)
	s2.Respond([]byte("+"))
	s2.Respond([]byte(serverFirst))
	if _, err := s2.Respond([]byte("v=" + base64.StdEncoding.EncodeToString([]byte("bogus")))); err == nil {
		t.Fatal("expected server-signature mismatch error")
	}
	_ = bytes.Equal // keep import if trimmed
}

func serverSignatureForTest(s *Scram) []byte {
	return hmacSum(s.serverKey, s.authMessage, s.h)
}

// TestScramRFC7677KnownAnswer pins the crypto to fixed external values from
// RFC 7677 §5 (SCRAM-SHA-256 example), independent of our own computation, so
// a spec-divergent transcription bug in the implementation would be caught
// even though it's internally self-consistent.
func TestScramRFC7677KnownAnswer(t *testing.T) {
	m, err := NewScram("SCRAM-SHA-256", "user", "pencil")
	if err != nil {
		t.Fatal(err)
	}
	s := m.(*Scram)
	s.nonce = "rOprNGfwEbeRWgbNEkqO"

	cf, err := s.Respond([]byte("+"))
	if err != nil {
		t.Fatal(err)
	}
	if string(cf) != "n,,n=user,r=rOprNGfwEbeRWgbNEkqO" {
		t.Fatalf("client-first: got %q", cf)
	}

	serverFirst := "r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"
	final, err := s.Respond([]byte(serverFirst))
	if err != nil {
		t.Fatal(err)
	}
	want := "c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,p=dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ="
	if string(final) != want {
		t.Fatalf("client-final:\n got  %q\n want %q", final, want)
	}

	// server-final must verify (empty ack response, no error).
	//
	// NOTE: RFC 7677 §3's published server-final-message is
	// "v=6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4=" — verified directly
	// against the RFC editor's raw text (curl https://www.rfc-editor.org/rfc/rfc7677.txt)
	// and cross-checked with an independent Python (hashlib/hmac) computation
	// of the same transcript, both of which agree with this Go implementation
	// byte-for-byte. An initial version of this test used a different tail
	// ("...mMUdzH2GFmOxhKb5hAxdgB4=") that does not appear in the RFC; that
	// was a transcription error in the test, not in the SCRAM implementation
	// (the client-first and client-final assertions above, which are also
	// pinned to the RFC, passed unmodified).
	resp, err := s.Respond([]byte("v=6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4="))
	if err != nil {
		t.Fatalf("valid RFC server signature rejected: %v", err)
	}
	if len(resp) != 0 {
		t.Fatalf("server-final ack should be empty, got %q", resp)
	}
}
