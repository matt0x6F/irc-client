package syms

import (
	"testing"

	"github.com/traefik/yaegi/interp"
)

// A script that only references the cascade type in a signature must type-check
// when the cascade symbols are injected — proving the import resolves.
//
// NOTE: yaegi extract uses the full module import path as the symbol-table key
// ("github.com/matt0x6f/irc-client/internal/script/cascade/cascade"), so the
// import string in scripts must use the full path.  Task 3's engine can expose
// a short alias ("cascade") via a separate symbol-table entry if desired.
func TestCascadeSymbolsResolveImport(t *testing.T) {
	i := interp.New(interp.Options{Unrestricted: false})
	if err := i.Use(Symbols); err != nil {
		t.Fatalf("Use(Symbols): %v", err)
	}
	src := `package main
import "github.com/matt0x6f/irc-client/internal/script/cascade"
func OnText(e cascade.TextEvent) { _ = e.Message }
`
	if _, err := i.Eval(src); err != nil {
		t.Fatalf("Eval referencing cascade.TextEvent failed: %v", err)
	}
}
