package script

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/traefik/yaegi/interp"

	"github.com/matt0x6f/irc-client/internal/script/cascade"
	"github.com/matt0x6f/irc-client/internal/script/syms"
)

// Script is a loaded, type-checked script package with its exported handlers.
type Script struct {
	handlers map[string]reflect.Value
}

// LoadPackage reads every .go file in dir as one Go package, type-checks it by
// evaluating it in a sandboxed Yaegi interpreter (cascade symbols only — no
// stdlib), and collects the exported main-package symbols as handlers.
func LoadPackage(dir string) (*Script, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("script %s: no .go files", dir)
	}

	i := interp.New(interp.Options{Unrestricted: false})
	if err := i.Use(syms.Table()); err != nil {
		return nil, fmt.Errorf("script %s: inject symbols: %w", dir, err)
	}

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if _, err := i.Eval(string(src)); err != nil {
			return nil, fmt.Errorf("script %s: load %s: %w", dir, filepath.Base(f), err)
		}
	}

	handlers := map[string]reflect.Value{}
	for _, pkgSyms := range i.Symbols("main") {
		for name, v := range pkgSyms {
			handlers[name] = v
		}
	}
	return &Script{handlers: handlers}, nil
}

// Has reports whether the script exported a handler with the given name.
func (s *Script) Has(handler string) bool {
	_, ok := s.handlers[handler]
	return ok
}

// DispatchText calls the script's OnText handler, if present.
func (s *Script) DispatchText(e cascade.TextEvent) {
	fn, ok := s.handlers["OnText"]
	if !ok {
		return
	}
	fn.Call([]reflect.Value{reflect.ValueOf(e)})
}
