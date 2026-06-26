package syms

import (
	"testing"

	"github.com/traefik/yaegi/interp"
)

// A script that only references the cascade type in a signature must type-check
// when the cascade symbols are injected via Table() — proving the clean
// `import "cascade"` resolves.
func TestCascadeSymbolsResolveImport(t *testing.T) {
	i := interp.New(interp.Options{Unrestricted: false})
	if err := i.Use(Table()); err != nil {
		t.Fatalf("Use(Table()): %v", err)
	}
	src := `package main
import "cascade"
func OnText(e cascade.TextEvent) { _ = e.Message }
`
	if _, err := i.Eval(src); err != nil {
		t.Fatalf("Eval referencing cascade.TextEvent via import \"cascade\" failed: %v", err)
	}
}
