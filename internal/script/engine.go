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
	"sort"
	"strings"

	"github.com/traefik/yaegi/interp"

	"github.com/matt0x6f/irc-client/cascade"
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

// call invokes the named handler with arg, if the handler is present.
func (s *Script) call(name string, arg any) {
	fn, ok := s.handlers[name]
	if !ok {
		return
	}
	fn.Call([]reflect.Value{reflect.ValueOf(arg)})
}

// DispatchText calls the script's OnText handler, if present.
func (s *Script) DispatchText(e cascade.TextEvent) { s.call("OnText", e) }

// DispatchNotice calls the script's OnNotice handler, if present.
func (s *Script) DispatchNotice(e cascade.NoticeEvent) { s.call("OnNotice", e) }

// DispatchJoin calls the script's OnJoin handler, if present.
func (s *Script) DispatchJoin(e cascade.JoinEvent) { s.call("OnJoin", e) }

// DispatchPart calls the script's OnPart handler, if present.
func (s *Script) DispatchPart(e cascade.PartEvent) { s.call("OnPart", e) }

// DispatchQuit calls the script's OnQuit handler, if present.
func (s *Script) DispatchQuit(e cascade.QuitEvent) { s.call("OnQuit", e) }

// DispatchKick calls the script's OnKick handler, if present.
func (s *Script) DispatchKick(e cascade.KickEvent) { s.call("OnKick", e) }

// DispatchNick calls the script's OnNick handler, if present.
func (s *Script) DispatchNick(e cascade.NickEvent) { s.call("OnNick", e) }

// HasSetup reports whether the script exported a Setup function.
func (s *Script) HasSetup() bool { return s.Has("Setup") }

// RunSetup calls the script's Setup(c) function, if present, passing the
// provided Client. Panics inside Setup are caught and returned as an error.
// Returns nil if Setup is absent.
func (s *Script) RunSetup(c *cascade.Client) (err error) {
	fn, ok := s.handlers["Setup"]
	if !ok {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Setup panic: %v", r)
		}
	}()
	fn.Call([]reflect.Value{reflect.ValueOf(c)})
	return nil
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
			return "", fmt.Errorf("read %s: %w", filepath.Base(f), err)
		}
		af, err := parser.ParseFile(fset, f, src, parser.ParseComments)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", filepath.Base(f), err)
		}
		pkgName = af.Name.Name
		for _, imp := range af.Imports {
			line := imp.Path.Value
			if imp.Name != nil { // aliased, dot, or blank import
				line = imp.Name.Name + " " + imp.Path.Value
			}
			imports[line] = struct{}{}
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
	keys := make([]string, 0, len(imports))
	for k := range imports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, imp := range keys {
		fmt.Fprintf(&out, "import %s\n", imp)
	}
	for _, b := range bodies {
		out.WriteString(b)
		out.WriteString("\n")
	}
	return out.String(), nil
}
