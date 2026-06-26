package script

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"

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

	merged, err := mergePackageSource(files)
	if err != nil {
		return nil, fmt.Errorf("script %s: %w", dir, err)
	}
	if _, err := i.Eval(merged); err != nil {
		return nil, fmt.Errorf("script %s: load: %w", dir, err)
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

// mergePackageSource concatenates a package's files into one source string,
// keeping a single `package` clause and de-duplicating import lines. Uses
// go/parser + go/printer so it's robust to formatting.
func mergePackageSource(files []string) (string, error) {
	fset := token.NewFileSet()
	imports := map[string]struct{}{}
	var pkgName string
	var bodies []string

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		af, err := parser.ParseFile(fset, f, src, parser.ParseComments)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", filepath.Base(f), err)
		}
		pkgName = af.Name.Name
		for _, imp := range af.Imports {
			imports[imp.Path.Value] = struct{}{}
		}
		// Re-print declarations except imports.
		for _, d := range af.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			var b strings.Builder
			if err := printer.Fprint(&b, fset, d); err != nil {
				return "", err
			}
			bodies = append(bodies, b.String())
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "package %s\n", pkgName)
	for imp := range imports {
		fmt.Fprintf(&out, "import %s\n", imp)
	}
	for _, b := range bodies {
		out.WriteString(b)
		out.WriteString("\n")
	}
	return out.String(), nil
}
