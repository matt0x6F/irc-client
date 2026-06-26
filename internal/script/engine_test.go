package script

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/script/cascade"
)

func TestLoadAndDispatchOnText(t *testing.T) {
	s, err := LoadPackage("testdata/greeter")
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if !s.Has("OnText") {
		t.Fatalf("expected OnText handler to be discovered")
	}

	var got []string
	s.DispatchText(cascade.NewTextEvent("bob", "!hello world", func(m string) { got = append(got, m) }))

	if len(got) != 1 || got[0] != "Hi bob" {
		t.Fatalf("reply = %v; want [Hi bob]", got)
	}
}
