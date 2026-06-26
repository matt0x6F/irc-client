# Cascade Scripting v1 — PR3: Extract `cascade` into its own SDK module

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Move the `cascade` script API out of `internal/script/cascade` into a nested Go module at `github.com/matt0x6f/irc-client/cascade`, so user scripts (and their external gopls) can resolve `import "github.com/matt0x6f/irc-client/cascade"` from a published tag (`cascade/v1.0.0`), while the Yaegi runtime resolves the same path from its committed symbol table. Pure refactor — no new runtime behavior; the guarantee is that nothing regresses.

**Architecture:** A nested module (its own `go.mod`, zero external deps) keeps a script's dependency graph tiny and versions the script API independently of the app. The app's root module references it via `require … + replace … => ./cascade` for local builds. Because scripts now use the real module path, the PR2 short-import alias (`syms.Table()` aliasing `"cascade/cascade"`) is removed — gopls and Yaegi agree on the full path with no aliasing.

**Tech Stack:** Go multi-module repo; `yaegi extract`.

## Global Constraints

- **Module path is lowercase:** `github.com/matt0x6f/irc-client` (per the root `go.mod`). The new module is `github.com/matt0x6f/irc-client/cascade`. Use this exact casing everywhere.
- **Go tests:** `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1` must stay green. gofmt-clean; `go vet` passes.
- **Both modules must build:** `(cd cascade && go build ./...)` and `go build ./internal/...` from the root.
- **The symbol table is committed/generated** (like Wails bindings) — regenerate it, don't hand-edit the `func init()` body.
- **No behavior change.** This PR only moves code and repaths imports. Do not add features, change the `cascade` API surface, or touch `internal/extension`, `internal/plugin`, or `internal/events`.
- **`cascade/v1.0.0` is NOT git-tagged in this PR** — the app builds via the local `replace`. The tag is pushed at release time. PR3 only establishes the module and the `require …/cascade v1.0.0` line.

---

## Current state (from merged PR1/PR2)

- `internal/script/cascade/events.go` + `events_test.go` — the API (`TextEvent`, `NewTextEvent`; imports only `strings`).
- `internal/script/syms/symbols.go` — `var Symbols` + `Table()` with the short-alias consts/logic (TO BE SIMPLIFIED).
- `internal/script/syms/cascade_extract.go` — generated; key `github.com/matt0x6f/irc-client/internal/script/cascade/cascade` (TO BE REGENERATED).
- `internal/script/engine.go` + `engine_test.go` — import `.../internal/script/cascade`.
- `internal/script/testdata/{greeter,multifile,typeerror,forbidden,aliasedimport}/*.go` — fixtures using the short `import "cascade"` (TO BE REPATHED to the full module path).
- `Taskfile.yml` `script-symbols` target — extracts from `.../internal/script/cascade`.

---

## PR3 — Task 1: Extract cascade into the nested SDK module (atomic refactor)

**Files:**
- Create: `cascade/go.mod`
- Move: `internal/script/cascade/events.go` → `cascade/events.go`; `internal/script/cascade/events_test.go` → `cascade/events_test.go` (then remove the empty `internal/script/cascade/`).
- Modify: root `go.mod` (+ `go.sum` via tidy); `internal/script/engine.go`; `internal/script/engine_test.go`; `internal/script/syms/symbols.go`; `internal/script/syms/symbols_test.go`; `internal/script/syms/cascade_extract.go` (regenerated); `Taskfile.yml`; all 5 fixture dirs under `internal/script/testdata/`.

**Interfaces:**
- The `cascade` package API is UNCHANGED (`TextEvent`, `NewTextEvent`, methods). Only its import path changes: `github.com/matt0x6f/irc-client/internal/script/cascade` → `github.com/matt0x6f/irc-client/cascade`.
- `syms.Table()` keeps its signature `func Table() interp.Exports` but its body becomes `return Symbols` (no aliasing). The engine continues to call `syms.Table()`.

> **Note on ordering:** this refactor leaves the repo non-compiling midway, so do the moves + repaths + regeneration as one unit, then verify. Run the full verification (Step 8) only at the end. Commit once at the end (or as two commits: the move+repath, then the regenerate+fixtures — your choice, but the suite must be green at HEAD).

- [ ] **Step 1: Record the green baseline**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/... -count=1`
Expected: all PASS (this is the behavior we must preserve).

- [ ] **Step 2: Create the nested module and move the source**

```bash
mkdir -p cascade
git mv internal/script/cascade/events.go cascade/events.go
git mv internal/script/cascade/events_test.go cascade/events_test.go
rmdir internal/script/cascade
```
Create `cascade/go.mod` (match the root module's Go version — read it from the root `go.mod`'s `go` line):
```
module github.com/matt0x6f/irc-client/cascade

go <same version as root go.mod>
```
The `cascade` package imports only `strings` (stdlib), so no `require` block is needed.

- [ ] **Step 3: Wire the root module to the nested one**

Edit the root `go.mod` to add (a `replace` to the local path so the app builds against in-repo source; the `require` version is the SDK's first tag):
```
require github.com/matt0x6f/irc-client/cascade v1.0.0

replace github.com/matt0x6f/irc-client/cascade => ./cascade
```
Then run `go mod tidy`. Verify `git diff go.mod` shows ONLY the cascade require/replace additions (plus any tidy reordering) — if tidy pulls or drops unrelated modules, stop and report.

- [ ] **Step 4: Repath the app's imports of cascade**

In `internal/script/engine.go` and `internal/script/engine_test.go`, change the import
`github.com/matt0x6f/irc-client/internal/script/cascade` → `github.com/matt0x6f/irc-client/cascade`.
(The package name stays `cascade`, so only the import path string changes; usages like `cascade.TextEvent` are unchanged.)

- [ ] **Step 5: Regenerate the symbol table for the new path**

Run (the Taskfile target after you update it in Step 7, or directly):
```bash
go run github.com/traefik/yaegi/cmd/yaegi extract -name syms github.com/matt0x6f/irc-client/cascade
mv github.com_matt0x6f_irc-client_cascade.go internal/script/syms/cascade_extract.go
```
(If the emitted filename differs, adjust the `mv`.) The regenerated file's key MUST now be `github.com/matt0x6f/irc-client/cascade/cascade`. Verify with `grep 'Symbols\[' internal/script/syms/cascade_extract.go`.

- [ ] **Step 6: Remove the short-import alias from `syms`**

In `internal/script/syms/symbols.go`, delete the `cascadeFullKey`/`cascadeShortKey` consts and simplify `Table()` to:
```go
// Table returns the cascade symbol table for injection via interp.Use.
func Table() interp.Exports {
	return Symbols
}
```
In `internal/script/syms/symbols_test.go`, change the embedded script source from `import "cascade"` to `import "github.com/matt0x6f/irc-client/cascade"` and keep injecting `Table()`; the test still asserts the import resolves via `i.Eval`.

- [ ] **Step 7: Update the Taskfile generation target**

In `Taskfile.yml`, the `script-symbols` target: change the extract path to `github.com/matt0x6f/irc-client/cascade` and the resulting `mv` source filename to match (`github.com_matt0x6f_irc-client_cascade.go`). Keep using `go run github.com/traefik/yaegi/cmd/yaegi`.

- [ ] **Step 8: Repath every test fixture to the full module path**

In each of these files, change `import "cascade"` → `import "github.com/matt0x6f/irc-client/cascade"` (preserving any alias, e.g. `import c "cascade"` → `import c "github.com/matt0x6f/irc-client/cascade"`):
- `internal/script/testdata/greeter/greeter.go`
- `internal/script/testdata/multifile/handler.go`
- `internal/script/testdata/typeerror/bad.go`
- `internal/script/testdata/forbidden/sneaky.go` (keep the `"os"` import — it must still fail)
- `internal/script/testdata/aliasedimport/main.go` (the `import c "..."` alias case)
(`multifile/helper.go` has no imports — leave it.)

- [ ] **Step 9: Full verification**

Run all of:
```bash
(cd cascade && go build ./... && CGO_ENABLED=1 go test ./... -count=1)   # nested module builds + its own test passes
go build ./internal/...                                                   # app packages build with new imports + replace
CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1                  # full internal suite green (script + extension + ...)
gofmt -l cascade/ internal/script/                                        # empty
CGO_ENABLED=1 go vet -tags fts5 ./internal/script/... ./internal/extension/...
```
Expected: nested module builds and its `TestTextEventReplyRoutesToSink` passes; app builds; the full internal suite passes (greeter/multifile/typeerror/forbidden/aliasedimport/symbols all green with the new import path); gofmt empty; vet clean.

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "refactor(cascade): extract script API into nested SDK module (cascade/v1.0.0)"
```

---

## PR3 Self-Review

- **Spec coverage:** cascade moved to nested module ✓; root `require`+`replace` ✓; imports repathed ✓; symbols regenerated for new key ✓; alias removed ✓; fixtures repathed ✓; both modules build + full suite green ✓.
- **No behavior change:** the `cascade` API surface is identical; only import paths and the (internal) symbol-table key changed. Every PR1 guarantee (load/dispatch, type-error-fails, forbidden-import-fails, multi-file, aliased-import) is re-proven by the unchanged tests passing against the new path.
- **Placeholder scan / type consistency:** `Table()` signature unchanged; engine still calls `syms.Table()`; module path lowercase `matt0x6f` throughout.

## After PR3

PR4 (script host/manager) wires the PR1 engine as an `extension.Host`: `Deliver(id, ev)` translates an `events.Event` into a `cascade.TextEvent` and calls `Script.DispatchText`, plus directory discovery, manifest (`// cascade:`) parsing **before** the package merge, lifecycle (load/reload/enable/disable), file-watch hot reload, and writing the scripts-dir `go.mod` (`require github.com/matt0x6f/irc-client/cascade v1.0.0`) so external gopls resolves the SDK.
