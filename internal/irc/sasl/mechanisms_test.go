package sasl

import (
	"bytes"
	"testing"
)

func TestPlainRespond(t *testing.T) {
	m := NewPlain("alice", "s3cret")
	got, err := m.Respond([]byte("+"))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("\x00alice\x00s3cret")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if m.Name() != "PLAIN" {
		t.Fatalf("name %q", m.Name())
	}
}

func TestExternalRespondEmpty(t *testing.T) {
	m := NewExternal()
	got, err := m.Respond([]byte("+"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("EXTERNAL response must be empty, got %q", got)
	}
	if m.Name() != "EXTERNAL" {
		t.Fatalf("name %q", m.Name())
	}
}

func TestForNetworkDefaultsAndErrors(t *testing.T) {
	if m, err := ForNetwork("", "u", "p"); err != nil || m.Name() != "PLAIN" {
		t.Fatalf("empty should default to PLAIN: %v %v", m, err)
	}
	if _, err := ForNetwork("NONSENSE", "u", "p"); err == nil {
		t.Fatal("expected error for unknown mechanism")
	}
}
