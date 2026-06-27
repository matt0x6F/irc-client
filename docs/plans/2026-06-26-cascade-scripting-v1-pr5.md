# Cascade Scripting v1 — PR5: Lifecycle & authoring (manifest · scaffolding · hot reload)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make scripts pleasant to author and maintain at runtime: parse the `// cascade:` manifest (name/description/permissions) from raw source; scaffold the scripts-dir `go.mod` so an external editor's gopls resolves the SDK; and hot-reload scripts on file changes (load/reload/unload) without restarting the app.

**Architecture:** The `script.Manager` (PR4) gains three capabilities. (1) `parseManifest` reads the `// cascade:` header from a script's *raw* `.go` files **before** the package merge (the merge strips file-level comments), populating the script's `extension.Extension` identity. (2) `scaffoldModule` writes a `go.mod` at the scripts-dir root. (3) A reload/unload core (`reload`/`unload`, built on the registry's replace + a new `Remove`) is driven by a thin fsnotify watcher. Per-script dispatch serialization + panic recovery (PR4) are unchanged.

**Tech Stack:** Go; `github.com/fsnotify/fsnotify` (new dep); the PR4 manager; the PR2 `extension` core.

## Global Constraints

- **Module path lowercase** `github.com/matt0x6f/irc-client`; cascade SDK `github.com/matt0x6f/irc-client/cascade`.
- **Tests:** `CGO_ENABLED=1 go test -tags fts5 <pkg> -count=1`; `internal/script` is concurrency-bearing → also `-race`. gofmt-clean; `go vet`.
- **Parse the manifest from RAW source, before the merge** — never from `mergePackageSource` output (it strips file-level comments).
- **Manifest is parsed, NOT enforced** — v1 is trusted-only; permissions are captured as data for v2/PR8, with no sandbox tiering yet.
- **Scaffolding must not break loading:** the `go.mod` lives at the scripts-dir *root*, not inside a script subdir, so it does not affect `LoadPackage` (which globs `*.go` in each subdir) or Yaegi (which uses the symbol table, not the module).
- **Don't regress PR4 safety:** dispatch stays per-script-serialized + panic-recovered; reload must be safe against an in-flight `Deliver`.
- **App `package main` needs a stub `frontend/dist` to build** in this worktree (Task 4): `mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html` (git-ignored; never commit).

---

## PR5 — File Structure

- `internal/extension/extension.go` — add `Description string` to `Extension` (Task 1).
- `internal/extension/registry.go` — add `Remove(id ID)` (Task 3).
- `internal/script/manifest.go` + `manifest_test.go` — manifest parsing (Task 1).
- `internal/script/testdata/withmanifest/withmanifest.go` — fixture with a `// cascade:` header (Task 1).
- `internal/script/manager.go` — wire manifest into `loadDir`; `scaffoldModule`; `reload`/`unload`; loadDir error-cleanup; `Watch`/`Close` (Tasks 1–4).
- `internal/script/manager_test.go` — scaffolding + reload/unload tests (Tasks 2–3).
- `app.go` — call `scriptMgr.Watch()` after `LoadAll` (Task 4).

---

## Task 1: Manifest parsing → Extension identity

**Files:**
- Modify: `internal/extension/extension.go` (add `Description string`)
- Create: `internal/script/manifest.go`, `internal/script/manifest_test.go`
- Create: `internal/script/testdata/withmanifest/withmanifest.go`
- Modify: `internal/script/manager.go` (`loadDir` uses the manifest)

**Interfaces:**
- Produces: `type Manifest struct { Name, Description string; Permissions []string }`; `func parseManifest(dir string) Manifest`. `extension.Extension` gains `Description string`.
- `loadDir` sets `ext.Name` (manifest name, else dir base), `ext.Description`, `ext.Perms` from the manifest.

- [ ] **Step 1: Add `Description` to `extension.Extension`**

In `internal/extension/extension.go`, add the field to the struct:
```go
type Extension struct {
	ID          ID
	Name        string
	Description string
	Kind        Kind
	Enabled     bool
	Status      Status
	Perms       []string
	Err         string
}
```

- [ ] **Step 2: Write the manifest fixture + failing test**

```go
// internal/script/testdata/withmanifest/withmanifest.go
// cascade:name Greeter Deluxe
// cascade:description Greets people who say hello.
// cascade:permissions storage, network
package main

import "github.com/matt0x6f/irc-client/cascade"

func OnText(e cascade.TextEvent) {
	if e.HasPrefix("!hello") {
		e.Reply("Hi " + e.Nick)
	}
}
```

```go
// internal/script/manifest_test.go
package script

import "testing"

func TestParseManifest(t *testing.T) {
	m := parseManifest("testdata/withmanifest")
	if m.Name != "Greeter Deluxe" {
		t.Fatalf("Name = %q; want Greeter Deluxe", m.Name)
	}
	if m.Description != "Greets people who say hello." {
		t.Fatalf("Description = %q", m.Description)
	}
	if len(m.Permissions) != 2 || m.Permissions[0] != "storage" || m.Permissions[1] != "network" {
		t.Fatalf("Permissions = %v; want [storage network]", m.Permissions)
	}
}

func TestParseManifestAbsent(t *testing.T) {
	m := parseManifest("testdata/greeter") // no header
	if m.Name != "" || m.Description != "" || len(m.Permissions) != 0 {
		t.Fatalf("expected empty manifest for greeter, got %+v", m)
	}
}
```

- [ ] **Step 3: Run — fails (parseManifest undefined)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestParseManifest -v`
Expected: FAIL (undefined).

- [ ] **Step 4: Implement `internal/script/manifest.go`**

```go
package script

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Manifest is the optional `// cascade:` header a script declares. It is parsed
// from raw source (before the package merge, which strips file-level comments)
// and is informational in v1 (permissions are captured but not enforced).
type Manifest struct {
	Name        string
	Description string
	Permissions []string
}

// parseManifest scans the .go files in dir for `// cascade:<key> <value>` lines.
// Unknown keys are ignored; the first value for a key wins.
func parseManifest(dir string) Manifest {
	var m Manifest
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			rest, ok := strings.CutPrefix(line, "//")
			if !ok {
				continue
			}
			rest = strings.TrimSpace(rest)
			kv, ok := strings.CutPrefix(rest, "cascade:")
			if !ok {
				continue
			}
			key, val, _ := strings.Cut(kv, " ")
			val = strings.TrimSpace(val)
			switch strings.TrimSpace(key) {
			case "name":
				if m.Name == "" {
					m.Name = val
				}
			case "description":
				if m.Description == "" {
					m.Description = val
				}
			case "permissions":
				if m.Permissions == nil {
					for _, p := range strings.Split(val, ",") {
						if p = strings.TrimSpace(p); p != "" {
							m.Permissions = append(m.Permissions, p)
						}
					}
				}
			}
		}
		fh.Close()
	}
	return m
}
```

- [ ] **Step 5: Wire the manifest into `loadDir`**

In `internal/script/manager.go`, change the `ext` construction in `loadDir` to consult the manifest:
```go
func (m *Manager) loadDir(dir string) {
	id := extension.ID(filepath.Base(dir))
	man := parseManifest(dir)
	name := man.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	ext := &extension.Extension{
		ID:          id,
		Name:        name,
		Description: man.Description,
		Kind:        extension.KindScript,
		Enabled:     true,
		Status:      extension.StatusLoaded,
		Perms:       man.Permissions,
	}
	// ... rest unchanged (LoadPackage, subscribe, register)
```

- [ ] **Step 6: Green + commit**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1` (extension + script green).
```bash
git add internal/extension/extension.go internal/script/manifest.go internal/script/manifest_test.go internal/script/testdata/withmanifest/ internal/script/manager.go
git commit -m "feat(script): parse // cascade: manifest into extension identity"
```

---

## Task 2: Scaffold the scripts-dir `go.mod`

**Files:**
- Modify: `internal/script/manager.go` (`cascadeSDKVersion` const, `scaffoldModule`, call in `LoadAll`)
- Modify: `internal/script/manager_test.go` (scaffolding test)

**Interfaces:**
- Produces: `func (m *Manager) scaffoldModule() error` — writes `<dir>/go.mod` iff absent. Called by `LoadAll` after the dir is ensured.

- [ ] **Step 1: Write the failing test**

```go
// add to internal/script/manager_test.go
func TestScaffoldModuleWritesGoMod(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(events.NewEventBus(), dir, func(int64, string, string) error { return nil })
	if err := m.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod not written: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "module cascade-scripts") ||
		!strings.Contains(s, "github.com/matt0x6f/irc-client/cascade "+cascadeSDKVersion) {
		t.Fatalf("go.mod missing module/require:\n%s", s)
	}
}

func TestScaffoldModuleDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	custom := "module custom\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(events.NewEventBus(), dir, func(int64, string, string) error { return nil })
	_ = m.LoadAll()
	b, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if string(b) != custom {
		t.Fatalf("existing go.mod was overwritten: %s", b)
	}
}
```
(Add `os`, `path/filepath`, `strings` to the test imports if not present.)

- [ ] **Step 2: Run — fails (cascadeSDKVersion/scaffold undefined or go.mod absent)**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestScaffoldModule -v`
Expected: FAIL.

- [ ] **Step 3: Implement in `internal/script/manager.go`**

```go
// cascadeSDKVersion is the cascade SDK module version the scaffolded scripts
// go.mod requires. Bump this when the app ships against a newer cascade tag.
const cascadeSDKVersion = "v1.0.0"

// scaffoldModule writes a go.mod at the scripts-dir root if one does not exist,
// so an external editor's gopls resolves `import ".../cascade"`. It never
// overwrites a user-edited go.mod. (Resolution requires the cascade/<ver> tag
// to be published; pre-release, gopls in the scripts dir cannot fetch it.)
func (m *Manager) scaffoldModule() error {
	path := filepath.Join(m.dir, "go.mod")
	if _, err := os.Stat(path); err == nil {
		return nil // exists; respect user edits
	}
	content := "module cascade-scripts\n\ngo 1.21\n\nrequire github.com/matt0x6f/irc-client/cascade " + cascadeSDKVersion + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}
```
In `LoadAll`, after `os.MkdirAll(m.dir, ...)` and before reading entries:
```go
	if err := m.scaffoldModule(); err != nil {
		return fmt.Errorf("scaffold scripts module: %w", err)
	}
```

- [ ] **Step 4: Green + commit**

Run: `CGO_ENABLED=1 go test -tags fts5 ./internal/script/ -run TestScaffoldModule -v`
```bash
git add internal/script/manager.go internal/script/manager_test.go
git commit -m "feat(script): scaffold scripts-dir go.mod for external gopls"
```

---

## Task 3: Reload / unload core

**Files:**
- Modify: `internal/extension/registry.go` (add `Remove`)
- Modify: `internal/extension/registry_test.go` (Remove test)
- Modify: `internal/script/manager.go` (`reload`, `unload`, loadDir error-cleanup)
- Modify: `internal/script/manager_test.go` (reload/unload tests)

**Interfaces:**
- Produces: `func (r *Registry) Remove(id ID)`; `func (m *Manager) reload(dir string)`; `func (m *Manager) unload(id extension.ID)`.
- Changed: `loadDir` deletes the `scripts` map entry on a load failure (so a reload-to-broken doesn't leave a stale runnable script).

- [ ] **Step 1: Add `Registry.Remove` + test**

```go
// internal/extension/registry.go
// Remove deletes an extension from the registry. A subsequent recipients() will
// not include it. No-op if absent.
func (r *Registry) Remove(id ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
}
```
```go
// add to internal/extension/registry_test.go
func TestRegistryRemove(t *testing.T) {
	r := NewRegistry()
	r.Register(&Extension{ID: "x", Enabled: true, Status: StatusLoaded}, nopHost{}, []string{"a"})
	r.Remove("x")
	if _, ok := r.Get("x"); ok {
		t.Fatalf("Get after Remove should be false")
	}
	if rc := r.recipients("a"); len(rc) != 0 {
		t.Fatalf("recipients after Remove = %v; want empty", rc)
	}
	r.Remove("x") // no-op, must not panic
}
```

- [ ] **Step 2: loadDir error-cleanup + reload/unload (failing test first)**

```go
// add to internal/script/manager_test.go
func TestManagerReloadPicksUpNewBehavior(t *testing.T) {
	dir := t.TempDir()
	sdir := filepath.Join(dir, "g")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(reply string) {
		src := "package main\nimport \"github.com/matt0x6f/irc-client/cascade\"\nfunc OnText(e cascade.TextEvent){ e.Reply(\"" + reply + "\") }\n"
		if err := os.WriteFile(filepath.Join(sdir, "g.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("first")

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, fs.send)
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	bus.EmitSync(msgEvent(1, "#c", "bob", "hi", ""))

	write("second")
	m.reload(sdir)
	bus.EmitSync(msgEvent(1, "#c", "bob", "hi", ""))

	fs.mu.Lock()
	defer fs.mu.Unlock()
	var sawSecond bool
	for _, s := range fs.sent {
		if s.message == "second" {
			sawSecond = true
		}
	}
	if !sawSecond {
		t.Fatalf("after reload expected a 'second' reply; got %+v", fs.sent)
	}
}

func TestManagerUnloadStopsDelivery(t *testing.T) {
	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, "testdata", fs.send)
	_ = m.LoadAll()
	m.unload(extension.ID("greeter"))
	if _, ok := m.registry().Get(extension.ID("greeter")); ok {
		t.Fatalf("greeter should be gone after unload")
	}
	bus.EmitSync(msgEvent(1, "#c", "bob", "!hello", ""))
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, s := range fs.sent {
		if s.message == "Hi bob" {
			t.Fatalf("greeter still delivered after unload")
		}
	}
}
```

- [ ] **Step 3: Implement reload/unload + loadDir cleanup**

In `loadDir`, the error branch must clear any prior loaded entry:
```go
	s, err := LoadPackage(dir)
	if err != nil {
		ext.Status = extension.StatusError
		ext.Err = err.Error()
		m.mu.Lock()
		delete(m.scripts, id) // a prior (working) version must not keep running
		m.mu.Unlock()
		m.reg.Register(ext, m, nil)
		return
	}
```
Add:
```go
// reload re-loads the script directory (used for hot reload + manual reload).
// loadDir already replaces the registry entry and scripts map slot, so reload
// is just a re-load by directory.
func (m *Manager) reload(dir string) { m.loadDir(dir) }

// unload removes a script entirely (used when its directory is deleted).
func (m *Manager) unload(id extension.ID) {
	m.mu.Lock()
	delete(m.scripts, id)
	m.mu.Unlock()
	m.reg.Remove(id)
}
```

- [ ] **Step 4: Green (incl -race) + commit**

Run:
```bash
CGO_ENABLED=1 go test -tags fts5 ./internal/extension/ -run TestRegistryRemove -v
CGO_ENABLED=1 go test -race -tags fts5 ./internal/script/ -count=1
```
Expected: all PASS, clean under -race.
```bash
git add internal/extension/registry.go internal/extension/registry_test.go internal/script/manager.go internal/script/manager_test.go
git commit -m "feat(script): reload/unload core (registry Remove, loadDir cleanup)"
```

---

## Task 4: fsnotify watcher + app wiring

**Files:**
- Modify: `go.mod`/`go.sum` (add `github.com/fsnotify/fsnotify`)
- Modify: `internal/script/manager.go` (`Watch`, `Close`, watch bookkeeping)
- Modify: `internal/script/manager_test.go` (watcher smoke test)
- Modify: `app.go` (call `Watch()` after `LoadAll`)

**Interfaces:**
- Produces: `func (m *Manager) Watch() error` (starts watching `m.dir` + each script subdir; on changes, reload/unload), `func (m *Manager) Close() error` (stops the watcher).

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/fsnotify/fsnotify@latest && go mod tidy`. Verify `git diff go.mod` adds only fsnotify (+ its deps).

- [ ] **Step 2: Implement the watcher in `internal/script/manager.go`**

```go
// (add import "github.com/fsnotify/fsnotify"; add fields: watcher *fsnotify.Watcher)

// Watch begins watching the scripts dir (and each script subdir) and hot-reloads
// on change. Safe to call once after LoadAll. Errors starting the watcher are
// returned; per-event errors are logged-and-continued by the run loop.
func (m *Manager) Watch() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	m.watcher = w
	// Watch the root (for new/removed script dirs) and each existing script dir.
	_ = w.Add(m.dir)
	if entries, err := os.ReadDir(m.dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				_ = w.Add(filepath.Join(m.dir, e.Name()))
			}
		}
	}
	go m.runWatch()
	return nil
}

// Close stops the watcher.
func (m *Manager) Close() error {
	if m.watcher != nil {
		return m.watcher.Close()
	}
	return nil
}

func (m *Manager) runWatch() {
	for {
		select {
		case ev, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.handleFSEvent(ev)
		case _, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// scriptDirFor maps a changed path to the script directory it belongs to (an
// immediate child of m.dir), or "" if the path is m.dir itself / unrelated.
func (m *Manager) scriptDirFor(path string) string {
	rel, err := filepath.Rel(m.dir, path)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
	if parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return ""
	}
	return filepath.Join(m.dir, parts[0])
}

func (m *Manager) handleFSEvent(ev fsnotify.Event) {
	// New directory under root: start watching it and load it.
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() && filepath.Dir(ev.Name) == m.dir {
			_ = m.watcher.Add(ev.Name)
			m.loadDir(ev.Name)
			return
		}
	}
	sub := m.scriptDirFor(ev.Name)
	if sub == "" {
		return
	}
	id := extension.ID(filepath.Base(sub))
	if _, err := os.Stat(sub); os.IsNotExist(err) {
		m.unload(id)
		return
	}
	if goFiles, _ := filepath.Glob(filepath.Join(sub, "*.go")); len(goFiles) > 0 {
		m.reload(sub)
	}
}
```
> A simple debounce is optional for v1; reloading twice on a rapid double-write is harmless (loadDir is idempotent-replace). Keep it simple. All identifiers are plain ASCII.

- [ ] **Step 3: Watcher smoke test (guarded, deterministic on its assertion)**

```go
// add to internal/script/manager_test.go
func TestWatchStartsAndCloses(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(events.NewEventBus(), dir, func(int64, string, string) error { return nil })
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}
	if err := m.Watch(); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
```
(Do NOT write a timing-dependent "edit a file and assert it reloaded within N ms" test — fsnotify event latency is platform-variable and flaky. The reload *logic* is covered deterministically by Task 3's `reload`/`unload` tests; this test only proves Watch/Close don't error.)

- [ ] **Step 4: Wire into app.go**

After `a.scriptMgr.LoadAll()` in `ServiceStartup`, start the watcher (log on error, non-fatal):
```go
	if err := a.scriptMgr.Watch(); err != nil {
		logger.Log.Warn().Err(err).Msg("failed to start script watcher")
	}
```

- [ ] **Step 5: Verify (stub dist) + full suite + commit**

Run:
```bash
mkdir -p frontend/dist && printf '<!doctype html>' > frontend/dist/index.html
go build .
CGO_ENABLED=1 go test -race -tags fts5 ./internal/script/ -count=1
CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1
gofmt -l app.go internal/script/
```
Expected: app builds; script green under -race; full internal suite green; gofmt empty. (Do NOT commit `frontend/dist`.)
```bash
git add go.mod go.sum internal/script/manager.go internal/script/manager_test.go app.go
git commit -m "feat(script): fsnotify hot reload + app wiring"
```

---

## PR5 Self-Review

- **Spec coverage:** manifest parsed from raw source → Name/Description/Perms ✓ (T1); `go.mod` scaffolding (idempotent) ✓ (T2); reload/unload core + `Registry.Remove` + loadDir cleanup ✓ (T3); fsnotify watcher + app wiring ✓ (T4).
- **Deferral honored:** enable/disable persistence is the ONLY deferral (→ PR8, with its UI/Wails setter). Manifest is parsed (not deferred), permissions captured-not-enforced.
- **Safety:** PR4's per-script serialization + panic recovery untouched; reload replaces via the registry's pinned replace semantics; in-flight `Deliver` holds its own `*loaded` ref so a concurrent reload is safe.
- **Type consistency:** `Extension.Description` used in loadDir; `cascadeSDKVersion` in scaffold + test; `reload`=`loadDir`.

## After PR5 — remaining deferred work

| Deferred | Where |
|----------|-------|
| **Enable/disable persistence** (SQLite) + the Wails setter/UI that drives it | **PR8** |
| **Permission enforcement** (sandbox tiering by granted perms; the manifest's `permissions` become load-bearing) | **v2** |
| **Full watchdog** (deadlines, runaway/memory, auto-disable after N panics) + echo-message loop guard | **PR6** |
| **More cascade actions** (Say/Join/Part/Notice, timers) + **more event types** (OnJoin/OnPart/**OnNotice** + the `messageType` producer discriminator) | **PR7** |
| **Frontend management & permissions surface** (Wails methods + Scripts panel) | **PR8** |
| **Cut the `cascade/v1.0.0` tag** (so the scaffolded go.mod resolves in external gopls) | **Release** |
