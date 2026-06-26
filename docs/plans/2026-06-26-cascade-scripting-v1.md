# Cascade Scripting v1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the in-process, statically-typed Cascade Scripting engine (v1 cut) — local trusted scripts only, no trust/permission apparatus yet — proving the Yaegi foundation before any IRC, UI, or persistence is wired.

**Architecture:** Scripts are literal Go packages (a directory of `.go` files) interpreted in-process by [Yaegi](https://github.com/traefik/yaegi). A generated symbol table exposes the typed `cascade` API and *nothing else* — that omission is the capability sandbox. The engine loads a script package by `Eval`-ing its source (which type-checks it), discovers exported handlers via `i.Symbols("main")`, and dispatches typed events to them via `reflect`. Design doc: [2026-06-26-cascade-scripting.md](2026-06-26-cascade-scripting.md).

**Tech Stack:** Go + Yaegi (`github.com/traefik/yaegi/interp`), the existing event bus (`internal/events/bus.go`), SQLite/SQLC storage (later PRs), Wails v3 + React 19 frontend (later PRs).

## Global Constraints

Every task's requirements implicitly include these (copied from `CLAUDE.md` and the design doc):

- **Go tests** run with `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1`. The `fts5` tag is required repo-wide even where unused.
- **gofmt clean** (`gofmt -l` must be empty) and **`go vet`** pass — CI gates on both.
- **Wails-bound methods** live on the `App` struct in `package main`; pass timestamps as ISO strings, never `time.Time` (later PRs).
- **DB changes** edit `internal/storage/schema.sql` + `internal/storage/queries/`, then `task sqlc-generate` (later PRs).
- **Yaegi is authoritative.** The `Eval`/load result is the source of truth for whether a script is valid; nothing else (no second type-checker) gates loading.
- **Restricted baseline = no stdlib injection.** v1 scripts get *only* the `cascade` symbol table. Do **not** call `i.Use(stdlib.Symbols)`. Absence is the sandbox.
- **No generics in the `cascade` surface** (Yaegi's weakest area) — concrete `func(<Event>)` signatures only.

---

## v1 PR Stack (roadmap)

v1 lands as a stack of PRs into a long-lived `cascade-scripting` integration branch (squash-merged to `main` at the end; see the stacked-integration workflow). Each PR is independently testable. **This document fully specifies PR1**; PR2+ are planned just-in-time once PR1 locks the Yaegi behavior their exact code depends on.

| PR | Deliverable | Why this slice |
|----|-------------|----------------|
| **PR1** *(this doc)* | **Yaegi foundation spike**: `cascade` API skeleton, generated symbol table, engine that loads a directory-as-package, type-checks it, and dispatches a typed event to an exported handler. Proven by Go tests incl. multi-file + sandbox. | De-risks the single biggest unknown. Everything below depends on its verified API. |
| PR2 | Shared `internal/extension/` core slice: `Extension` identity/registry + event router + `Host.Deliver` seam. | The plumbing scripts (and later plugins) plug into. |
| PR3 | Script host/manager: module scaffolding, directory discovery, lifecycle (load/reload/enable/disable), file-watch hot reload. | Turns the engine into a managed, reloadable subsystem. |
| PR4 | Resource watchdog: per-dispatch `context` deadline, panic recovery, runaway isolation + auto-disable. | The always-on safety floor (§5.2 of the design). |
| PR5 | Baseline `cascade` actions wired to IRC: `Reply`/`Say`/`Join`/`Part` via the same paths as `app_commands.go`; timers (`Every`/`After`); more event types. | Makes scripts actually *do* things. |
| PR6 | Wails-bound `App` methods + frontend management surface (inventory, enable/disable, load errors, "New script" scaffolder). | The primary app-owned UX. |

> **Note (scope honesty):** PR1 deliberately has **no IRC, no UI, no persistence, and no watchdog**. It proves load + type-check + dispatch + sandbox + multi-file in pure Go tests. This keeps the riskiest unknown isolated and fast to iterate.

---

## PR1 — File Structure

- `internal/script/cascade/events.go` — the typed `TextEvent` (v1 minimal API surface scripts import).
- `internal/script/cascade/events_test.go` — pure-Go test of the event seam (no Yaegi).
- `internal/script/syms/symbols.go` — hand-written `var Symbols = interp.Exports{}` bootstrap.
- `internal/script/syms/cascade_extract.go` — **generated** by `yaegi extract`; populates `Symbols` for the `cascade` package.
- `internal/script/engine.go` — `LoadPackage(dir)` + `(*Script).DispatchText(...)`.
- `internal/script/engine_test.go` — load/dispatch/type-error/sandbox/multi-file tests.
- `internal/script/testdata/` — fixture script directories (one subdir per fixture).
- `Taskfile.yml` — add a `script-symbols` generation target.

> **Why `syms` is a separate package from `cascade`:** `yaegi extract` generates code that does `reflect.ValueOf(cascade.TextEvent...)`, so the generated file must *import* `cascade`. If it lived in `cascade` itself that's a self-import cycle. A sibling `syms` package gives a clean DAG: `engine → syms → cascade`, and `engine → cascade`. (Minor refinement of the design doc's indicative `cascade/symbols.go`.)

---

## PR1 — Tasks

### Task 1: `cascade` API skeleton + event seam

**Files:**
- Create: `internal/script/cascade/events.go`
- Test: `internal/script/cascade/events_test.go`

**Interfaces:**
- Produces: `cascade.TextEvent` (exported fields `Nick`, `Message string`); methods `HasPrefix(string) bool`, `Arg(int) string`, `Reply(string)`; constructor `NewTextEvent(nick, message string, reply func(string)) TextEvent` (used by the host to wire `Reply` to a sink — scripts never call it).

- [ ] **Step 1: Write the failing test**

```go
// internal/script/cascade/events_test.go
package cascade

import "testing"

func TestTextEventReplyRoutesToSink(t *testing.T) {
	var got []string
	e := NewTextEvent("alice", "!hello there", func(s string) { got = append(got, s) })

	if !e.HasPrefix("!hello") {
		t.Fatalf("HasPrefix(!hello) = false, want true")
	}
	if e.Arg(1) != "!hello" || e.Arg(2) != "there" {
		t.Fatalf("Arg(1),Arg(2) = %q,%q; want %q,%q", e.Arg(1), e.Arg(2), "!hello", "there")
	}
	if e.Arg(99) != "" {
		t.Fatalf("Arg(99) = %q; want empty", e.Arg(99))
	}

	e.Reply("hi alice")
	if len(got) != 1 || got[0] != "hi alice" {
		t.Fatalf("reply sink = %v; want [hi alice]", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/cascade/ -run TestTextEventReplyRoutesToSink -v`
Expected: FAIL — `undefined: NewTextEvent` (package doesn't compile yet).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/script/cascade/events.go
package cascade

import "strings"

// TextEvent is delivered to OnText handlers. The host constructs it via
// NewTextEvent and wires Reply to a sink; the script only reads fields and replies.
type TextEvent struct {
	Nick    string
	Message string

	replyFn func(string)
}

// NewTextEvent is the host-side constructor. Scripts never call it.
func NewTextEvent(nick, message string, reply func(string)) TextEvent {
	return TextEvent{Nick: nick, Message: message, replyFn: reply}
}

// HasPrefix reports whether the message starts with s.
func (e TextEvent) HasPrefix(s string) bool { return strings.HasPrefix(e.Message, s) }

// Arg returns the n-th whitespace-separated field (1-based), or "" if out of range.
func (e TextEvent) Arg(n int) string {
	f := strings.Fields(e.Message)
	if n < 1 || n > len(f) {
		return ""
	}
	return f[n-1]
}

// Reply sends a reply to the message's origin. No-op if no sink is wired.
func (e TextEvent) Reply(msg string) {
	if e.replyFn != nil {
		e.replyFn(msg)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/cascade/ -run TestTextEventReplyRoutesToSink -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/script/cascade/
git commit -m "feat(script): add cascade.TextEvent API skeleton + reply seam"
```

---

### Task 2: Generated symbol table + Yaegi resolves `import "cascade"`

**Files:**
- Create: `internal/script/syms/symbols.go`
- Create (generated): `internal/script/syms/cascade_extract.go`
- Modify: `Taskfile.yml` (add `script-symbols` target)
- Modify: `go.mod`/`go.sum` (add `github.com/traefik/yaegi`)
- Test: `internal/script/syms/symbols_test.go`

**Interfaces:**
- Consumes: `cascade.TextEvent` (Task 1).
- Produces: `syms.Symbols` of type `interp.Exports` (`map[string]map[string]reflect.Value`), with key `"cascade/cascade"` populated so interpreted code can `import "cascade"`.

- [ ] **Step 1: Add the Yaegi dependency**

Run:
```bash
go get github.com/traefik/yaegi@latest
go install github.com/traefik/yaegi/cmd/yaegi@latest
```
Expected: `go.mod` now requires `github.com/traefik/yaegi`.

- [ ] **Step 2: Write the failing test**

```go
// internal/script/syms/symbols_test.go
package syms

import (
	"testing"

	"github.com/traefik/yaegi/interp"
)

// A script that only references the cascade type in a signature must type-check
// when the cascade symbols are injected — proving import "cascade" resolves.
func TestCascadeSymbolsResolveImport(t *testing.T) {
	i := interp.New(interp.Options{Unrestricted: false})
	if err := i.Use(Symbols); err != nil {
		t.Fatalf("Use(Symbols): %v", err)
	}
	src := `package main
import "cascade"
func OnText(e cascade.TextEvent) { _ = e.Message }
`
	if _, err := i.Eval(src); err != nil {
		t.Fatalf("Eval referencing cascade.TextEvent failed: %v", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/syms/ -run TestCascadeSymbolsResolveImport -v`
Expected: FAIL — package `syms` doesn't exist / `Symbols` undefined.

- [ ] **Step 4: Write the `Symbols` bootstrap**

```go
// internal/script/syms/symbols.go
package syms

import "github.com/traefik/yaegi/interp"

// Symbols is the Yaegi symbol table for the cascade API. Generated extract
// files populate it in their init(). This is the ENTIRE surface a v1 script
// sees — no stdlib is injected, so the absence is the capability sandbox.
var Symbols = interp.Exports{}
```

- [ ] **Step 5: Generate the extract file**

Run:
```bash
yaegi extract -name syms github.com/matt0x6f/irc-client/internal/script/cascade
mv github.com_matt0x6f_irc-client_internal_script_cascade.go internal/script/syms/cascade_extract.go
```
Expected: `cascade_extract.go` contains `func init() { Symbols["cascade/cascade"] = map[string]reflect.Value{ "TextEvent": reflect.ValueOf((*cascade.TextEvent)(nil)), "NewTextEvent": reflect.ValueOf(cascade.NewTextEvent), ... } }`.

> **Spike checkpoint:** confirm the generated key is `"cascade/cascade"` (Yaegi's `<import-path>/<pkg-name>` convention — verified against Yaegi docs). If `yaegi extract` emits a different key, the fixtures' `import` path must match it. Adjust and note the real value here.

- [ ] **Step 6: Add the Taskfile target**

```yaml
# Taskfile.yml — under tasks:
  script-symbols:
    desc: Regenerate the Yaegi symbol table for the cascade scripting API
    cmds:
      - yaegi extract -name syms github.com/matt0x6f/irc-client/internal/script/cascade
      - mv github.com_matt0x6f_irc-client_internal_script_cascade.go internal/script/syms/cascade_extract.go
```

- [ ] **Step 7: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/syms/ -run TestCascadeSymbolsResolveImport -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum Taskfile.yml internal/script/syms/
git commit -m "feat(script): generate cascade Yaegi symbol table; resolve import \"cascade\""
```

---

### Task 3: Engine — load a single-file package and dispatch to `OnText`

**Files:**
- Create: `internal/script/engine.go`
- Create: `internal/script/testdata/greeter/greeter.go` (fixture)
- Test: `internal/script/engine_test.go`

**Interfaces:**
- Consumes: `cascade.TextEvent`, `cascade.NewTextEvent` (Task 1); `syms.Symbols` (Task 2).
- Produces:
  - `func LoadPackage(dir string) (*Script, error)` — reads every `*.go` in `dir`, news a sandboxed interp, injects `syms.Symbols`, `Eval`s the source, collects exported `main` symbols.
  - `func (s *Script) DispatchText(e cascade.TextEvent)` — calls the script's `OnText` if present.
  - `func (s *Script) Has(handler string) bool`.

- [ ] **Step 1: Write the fixture**

```go
// internal/script/testdata/greeter/greeter.go
package main

import "cascade"

func OnText(e cascade.TextEvent) {
	if e.HasPrefix("!hello") {
		e.Reply("Hi " + e.Nick)
	}
}
```

- [ ] **Step 2: Write the failing test**

```go
// internal/script/engine_test.go
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestLoadAndDispatchOnText -v`
Expected: FAIL — `undefined: LoadPackage`.

- [ ] **Step 4: Write the engine**

```go
// internal/script/engine.go
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
	if err := i.Use(syms.Symbols); err != nil {
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
```

> **Spike checkpoint:** `i.Symbols("main")` returns `interp.Exports` keyed by package path. The loop above is key-agnostic (it flattens whatever packages are returned), which is robust if the key is `"main"` or `"main/main"`. If discovery returns nothing, verify whether the exported funcs require the interp to have evaluated a `package main` clause vs. bare decls — adjust the fixture/Eval accordingly and record the finding.

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestLoadAndDispatchOnText -v`
Expected: PASS — reply sink captured `Hi bob`.

- [ ] **Step 6: Commit**

```bash
git add internal/script/engine.go internal/script/engine_test.go internal/script/testdata/greeter/
git commit -m "feat(script): load directory-as-package and dispatch typed event to OnText"
```

---

### Task 4: Type errors fail at load (static-check guarantee)

**Files:**
- Create: `internal/script/testdata/typeerror/bad.go` (fixture)
- Modify: `internal/script/engine_test.go` (add test)

**Interfaces:**
- Consumes: `LoadPackage` (Task 3). No new production code — this proves an existing guarantee.

- [ ] **Step 1: Write the broken fixture**

```go
// internal/script/testdata/typeerror/bad.go
package main

import "cascade"

func OnText(e cascade.TextEvent) {
	// Type error: assigning a string method result to an int.
	var n int = e.Message
	_ = n
}
```

- [ ] **Step 2: Write the failing test**

```go
// add to internal/script/engine_test.go
func TestTypeErrorFailsAtLoad(t *testing.T) {
	s, err := LoadPackage("testdata/typeerror")
	if err == nil {
		t.Fatalf("expected load to fail on a type error, got nil error (script=%v)", s)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestTypeErrorFailsAtLoad -v`
Expected: FAIL if (and only if) the fixture doesn't actually error — confirms the test is meaningful. If it passes immediately, good: the guarantee already holds. Either way, leave the test in.

> Note: this task has no implementation step — `LoadPackage` already returns Yaegi's `Eval` error. The test *pins* the "fails at load, not at 2am" guarantee so a future refactor can't silently swallow it.

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestTypeErrorFailsAtLoad -v`
Expected: PASS — `LoadPackage` returned a non-nil error.

- [ ] **Step 5: Commit**

```bash
git add internal/script/testdata/typeerror/ internal/script/engine_test.go
git commit -m "test(script): pin static type-error-fails-at-load guarantee"
```

---

### Task 5: Capability sandbox baseline — forbidden imports fail to load

**Files:**
- Create: `internal/script/testdata/forbidden/sneaky.go` (fixture)
- Modify: `internal/script/engine_test.go` (add test)

**Interfaces:**
- Consumes: `LoadPackage` (Task 3). Proves "absence is the sandbox" — no `os` symbol means `import "os"` cannot compile.

- [ ] **Step 1: Write the forbidden fixture**

```go
// internal/script/testdata/forbidden/sneaky.go
package main

import (
	"cascade"
	"os"
)

func OnText(e cascade.TextEvent) {
	os.Exit(1)
}
```

- [ ] **Step 2: Write the failing test**

```go
// add to internal/script/engine_test.go
func TestForbiddenImportFailsToLoad(t *testing.T) {
	if _, err := LoadPackage("testdata/forbidden"); err == nil {
		t.Fatalf("expected import \"os\" to fail (not in symbol table); got nil error")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestForbiddenImportFailsToLoad -v`
Expected: FAIL only if `os` somehow resolves. It should already PASS once written — that's the point.

> **Spike checkpoint:** confirm Yaegi rejects `import "os"` when stdlib is not injected AND `Unrestricted: false`. If it unexpectedly resolves a built-in, that's a sandbox hole to close before PR4 — record it loudly here.

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestForbiddenImportFailsToLoad -v`
Expected: PASS — load failed because `os` is absent from the symbol table.

- [ ] **Step 5: Commit**

```bash
git add internal/script/testdata/forbidden/ internal/script/engine_test.go
git commit -m "test(script): prove forbidden imports fail to load (absence is the sandbox)"
```

---

### Task 6: Multi-file package loads as one unit (the directory-as-package spike)

**Files:**
- Create: `internal/script/testdata/multifile/handler.go` (fixture)
- Create: `internal/script/testdata/multifile/helper.go` (fixture)
- Modify: `internal/script/engine_test.go` (add test)
- Possibly modify: `internal/script/engine.go` (only if sequential `Eval` doesn't accumulate)

**Interfaces:**
- Consumes: `LoadPackage` (Task 3). Verifies a handler in one file can call a helper defined in another file of the same package.

- [ ] **Step 1: Write the two-file fixture**

```go
// internal/script/testdata/multifile/handler.go
package main

import "cascade"

func OnText(e cascade.TextEvent) {
	e.Reply(greeting(e.Nick))
}
```

```go
// internal/script/testdata/multifile/helper.go
package main

func greeting(nick string) string { return "yo " + nick }
```

- [ ] **Step 2: Write the failing test**

```go
// add to internal/script/engine_test.go
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
```

- [ ] **Step 3: Run test to verify it fails or surfaces the strategy**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestMultiFilePackageLoads -v`
Expected: one of two outcomes —
  - **PASS:** sequential `Eval` of each file accumulates into the same `main` scope. The directory-as-package strategy is confirmed as-is. ✅
  - **FAIL** (e.g. "greeting undefined" or "package main redeclared"): sequential `Eval` doesn't merge files. Proceed to Step 4.

- [ ] **Step 4: If needed, implement the merge strategy**

Only if Step 3 failed. Replace the per-file `Eval` loop in `LoadPackage` with a single merged source: keep the first file's `package` clause, strip subsequent files' `package` clauses, and union their imports before one `Eval`. Reference implementation:

```go
// Replace the "for _, f := range files { ... Eval ... }" loop with:
merged, err := mergePackageSource(files)
if err != nil {
	return nil, fmt.Errorf("script %s: %w", dir, err)
}
if _, err := i.Eval(merged); err != nil {
	return nil, fmt.Errorf("script %s: load: %w", dir, err)
}
```

```go
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
```
Add imports to `engine.go`: `go/ast`, `go/parser`, `go/printer`, `go/token`, `strings`.

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestMultiFilePackageLoads -v`
Expected: PASS — reply is `yo cara`.

- [ ] **Step 6: Run the full PR1 suite + gofmt/vet**

Run:
```bash
CGO_ENABLED=1 go test -tags fts5 ./internal/script/... -count=1
gofmt -l internal/script/
go vet ./internal/script/...
```
Expected: all tests pass; `gofmt -l` prints nothing; vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/script/
git commit -m "feat(script): verify directory-as-package multi-file loading (spike)"
```

---

## PR1 Self-Review

Run against the design doc + spec:

- **Spec coverage (v1 PR1 slice):** literal-Go via Yaegi ✓ (Task 3); static type-check at load ✓ (Task 4); capability sandbox = symbol-table absence ✓ (Task 5); directory-as-package incl. multi-file ✓ (Tasks 3, 6); generated symbol table ✓ (Task 2); no stdlib injection ✓ (Global Constraints + Task 3). Out-of-PR1-scope items (trust, watchdog, IRC, UI, persistence, hot reload) are explicitly deferred to PR2–PR6.
- **Placeholder scan:** the three "Spike checkpoint" notes are verification instructions for genuinely Yaegi-dependent behavior, not deferred work — each has a concrete expected outcome and a fallback (Task 6 Step 4). No `TODO`/`TBD` in code.
- **Type consistency:** `cascade.TextEvent`/`NewTextEvent` (Task 1) are used verbatim in Tasks 3–6; `syms.Symbols` (Task 2) is consumed in Task 3; `LoadPackage`/`Has`/`DispatchText` signatures are stable across all tasks.

## After PR1

PR1 locks: the real `i.Symbols("main")` key shape, the multi-file strategy (sequential vs. merged), and the import-path/key the symbol table uses. **PR2's plan is written against those verified facts** — do not pre-write PR2 code until PR1 is green and merged into the integration branch.
