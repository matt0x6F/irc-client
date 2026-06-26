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

func TestTypeErrorFailsAtLoad(t *testing.T) {
	s, err := LoadPackage("testdata/typeerror")
	if err == nil {
		t.Fatalf("expected load to fail on a type error, got nil error (script=%v)", s)
	}
}

func TestForbiddenImportFailsToLoad(t *testing.T) {
	if _, err := LoadPackage("testdata/forbidden"); err == nil {
		t.Fatalf("expected import \"os\" to fail (not in symbol table); got nil error")
	}
}

func TestMultiFilePackageLoads(t *testing.T) {
	s, err := LoadPackage("testdata/multifile")
	if err != nil {
		t.Fatalf("LoadPackage(multifile): %v", err)
	}
	var got []string
	s.DispatchText(cascade.NewTextEvent("cara", "!x", func(m string) { got = append(got, m) }))
	if len(got) != 1 || got[0] != "yo cara" {
		t.Fatalf("reply = %v; want [yo cara]", got)
	}
}

func TestAliasedImportSurvivesMerge(t *testing.T) {
	s, err := LoadPackage("testdata/aliasedimport")
	if err != nil {
		t.Fatalf("LoadPackage(aliasedimport): %v", err)
	}
	var got []string
	s.DispatchText(cascade.NewTextEvent("dee", "!x", func(m string) { got = append(got, m) }))
	if len(got) != 1 || got[0] != "aliased dee" {
		t.Fatalf("reply = %v; want [aliased dee]", got)
	}
}
