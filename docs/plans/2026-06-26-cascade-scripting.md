# Design: Cascade Scripting (embedded, statically-typed automation)

**Date:** 2026-06-26
**Status:** Approved (design)
**Topic:** An in-process, statically-typed scripting layer — mIRC-spirited automation that
lives *inside* the client, sitting one rung below the subprocess plugin system.

## Why

The plugin system (`internal/plugin/`) is heavyweight by design: every plugin is a
subprocess speaking JSON-RPC 2.0 over stdin/stdout (`internal/plugin/ipc.go`), registering
commands, event subscriptions, and UI metadata. That isolation is the right call for
substantial, shippable tools — but it sets a high floor. To automate *your own* client you
must write a real program, build a binary, and manage a process.

mIRC's scripting fills the opposite niche: in-process, zero-install, power-user automation
that lives in the client's own config — `on TEXT:`, aliases, timers, `$identifiers`. The
whole point is low ceremony.

**Cascade Scripting fills the gap between "type a `/command`" and "build and ship a plugin
subprocess."** It is explicitly *secondary* to plugins: when a script needs real isolation,
arbitrary dependencies, or genuinely hostile-input safety, that is a signal to graduate it
to a plugin.

## Design decisions (and why)

These were settled during brainstorming. Recorded here with rationale so the *why* survives.

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | **Typing model** | Full **static** type-checking — errors at load, not at runtime | The demanding option, but it means a typo or wrong-arg-count fails when the script loads, not at 2am when `on KICK` finally fires. |
| 2 | **Language / engine** | **Literal Go**, interpreted by [Yaegi](https://github.com/traefik/yaegi) | Inherits Go's *entire* type checker for free — zero grammar/parser/checker to build or maintain. Real interop: the `cascade.TextEvent` a script receives *is* the host struct, no binding layer. The language surface is durable; only the interpreter is a dependency. |
| 3 | **Trust model** | **Tiered** (capability via symbol table + always-on resource watchdog) | Reuses the mental model of the existing plugin `Permissions []string` field (`internal/plugin/protocol.go:50`). Capability and resource are *independent* dials (see §5). |
| 4 | **Event model** | **Hybrid** — convention (`OnText`) + optional `Setup(c)` | Bare `OnText` keeps the casual case mIRC-simple; `Setup(c)` unlocks timers, filters, multiple handlers, and dynamic wiring for power users. Both ride the existing event bus (`internal/events/bus.go`). |
| 5 | **Provenance** | **Both** — origin (quarantine bit) + manifest (declared permissions) | Origin decides *whether you're asked* to grant; the manifest decides *what you're asked about*. The manifest permission list *is* the symbol-table configuration (§5). |
| 6 | **Code organization** | **Shared `extension/` core + per-runtime hosts** | Plugins and scripts already share identity, trust, event routing, commands, and UI metadata — only the *runtime* differs. One core unifies the plumbing without forcing scripting through IPC, and makes the shared-sidecar future a host swap (see Runtime model + Phasing/v3). |
| 7 | **Script unit** | **A directory (a Go package)** — single file is the one-file case | Go's native unit for "files that belong together" is the package. Scales from a 5-line greeter to a multi-file script with no new mechanism (see §Script unit & authoring). |
| 8 | **Authoring** | **The user's own editor + gopls** — no in-app editor | Scaffolding the scripts dir as a real Go module makes external gopls deliver a *better* IDE than we could embed, for ~zero build cost. The app owns *management & permissions*, not editing. |

### Why not the alternatives

- **A purpose-built typed DSL** (e.g. on `expr-lang`): more mIRC-like syntax, but you own a
  grammar, type-checker, error messages, and docs forever. Rejected: too much to maintain
  for a *secondary* feature.
- **Dynamically-typed embeds** (Starlark, gopher-lua, Tengo, Risor): fail decision #1 — they
  check at runtime, not load.
- **JS via Goja + TypeScript**: "typed" only if you embed a real TS type-checker, and there
  is no robust pure-Go one (`esbuild` strips types, it does not verify them). Rejected.
- **Real Go compilation** (`plugin` buildmode / subprocess): the only way to fully escape
  Yaegi's two soft spots (§5) — but it *is* the plugin model. That is the graduation path,
  not the scripting layer.

## Goals / Non-goals

**Goals**
- Power users automate their own client with small, statically-checked Go snippets.
- Near-zero ceremony for the common case; full flexibility when needed.
- A script can never silently corrupt the client: load errors are surfaced, panics are
  recovered, runaways are isolated and disabled.
- Trust is explicit and visible, modeled on the existing plugin permission convention.

**Non-goals**
- Hard, preemptive sandboxing of *hostile* code. We contain blast radius, not adversaries
  (§5.2). Hostile-input safety is what the out-of-process plugin model is for.
- A package manager / script registry (future; not v1 or v2).
- Replacing plugins. Scripts are the lightweight tier, deliberately less capable.
- **An in-app code editor.** Scripts are real Go in a real module; the user's own editor +
  gopls is the authoring experience (see §Script unit & authoring). The app owns *which scripts
  load and what they may do*, not how they're typed.

## Runtime model: why in-process, and the subprocess escape hatch

The two soft spots of in-process Yaegi (§5.2: no goroutine preemption, no hard memory cap)
*are* solvable — by a process boundary, which is exactly why plugins are subprocesses. But
that boundary is not free, and its cost lands on the very properties that motivated choosing
literal-Go in the first place:

- A process wall forces **serialization.** The win in decision #2 — the `cascade.TextEvent` a
  script receives *is* the host struct, zero marshalling — disappears the moment events and
  actions cross the boundary as JSON-RPC. `e.Reply` stops being a call and becomes a round-trip.
- It costs **more** maintenance, not less: a wire protocol *and* a mirrored typed SDK, versus
  one interpreter.
- **Per-event latency** matters more for scripts than plugins: scripts hook every PRIVMSG;
  plugins are coarse-grained. An IPC hop per message is a real tax.

And the limit case is decisive: **one process per script *is* the plugin system** — arbitrary
code, own process, JSON-RPC, permissions, lifecycle. So the line between the tiers was never
"in-process vs. subprocess." It is **who provides the runtime:**

| | **Script** | **Plugin** |
|---|---|---|
| Runtime | borrows the client's (Yaegi runs it) | brings its own (compiled binary) |
| You ship | a `.go` snippet | an executable |
| Bought with trust | speed, zero ceremony, real interop | hard isolation, any language/deps |
| §5.2 limit | the price of admission | N/A — already isolated |

Read this way, **§5.2 is the defining trait of the lightweight tier, not a defect.** Hardening
scripting with subprocess isolation would erase the line that justifies two systems. So we keep
them distinct — and merge only the *plumbing*, not the runtimes (next section).

**The escape hatch (designed-for, not built in v1/v2):** because the `cascade` package is a
clean façade and both runtimes sit on a shared core, Yaegi can later move behind a single
**shared-sidecar** process — one script host the client can `Kill()`+restart, reloading the
survivors — *without changing what any script looks like.* Only the host binding flips from
direct-call to RPC. Details in Phasing/v3.

## Architecture

Two runtimes — the existing subprocess **plugins** and the new in-process **scripts** — share
one runtime-agnostic core. Only the *engine* and the *delivery mechanism* differ; identity,
trust, event routing, commands, and UI metadata are common.

```
internal/
  extension/            # SHARED CORE — runtime-agnostic
    extension.go        #   identity + status: id, name, kind{plugin|script}, enabled, tier, perms, error, runaway
    trust.go            #   permissions, grants, quarantine bit (shared; see §Trust)
    router.go           #   per-extension event subscription + permission-checked fan-out from events/bus.go; the Host.Deliver seam
    commands.go         #   slash-command registration into the existing command registry
    metadata.go         #   UI metadata registry (promoted from internal/plugin/metadata.go — both runtimes need it)
    registry.go         #   unified listing + status feed for the frontend
  plugin/               # HOST: subprocess runtime (existing) — adopts extension/ incrementally
    ...                 #   implements extension.Host; Deliver = JSON-RPC notify over stdin/stdout
  script/               # HOST: Yaegi runtime (new) — built on extension/ from day one
    cascade/            #   the typed API scripts import — the ONLY surface a restricted script sees
      client.go         #     Client, Setup entrypoint, On*/Every/Match registration
      events.go         #     TextEvent, JoinEvent, ... (typed event structs)
      actions.go        #     Reply, Send, Notice, Join, ... (host actions)
      store.go          #     KV facade (gated by `storage`)
      symbols.go        #     GENERATED by `yaegi extract` — the injected symbol table
    engine.go           #   Yaegi lifecycle: build symbol table → Compile() (type-check) → instantiate
    manager.go          #   discovery, load/reload/enable-disable, file-watch hot reload
    sandbox.go          #   per-permission symbol sets (capability) + watchdog dispatch (resource)
    store.go            #   per-script KV over SQLite
    manifest.go         #   parse `// cascade:` headers
                        #   implements extension.Host; Deliver = watchdog'd in-process call
```

The seam both hosts implement:
```go
type Host interface {
    Kind() string                            // "plugin" | "script"
    Deliver(id ExtensionID, ev Event) error  // RPC notify (plugin) OR watchdog'd call (script)
    Load(id ExtensionID) error
    Unload(id ExtensionID) error
}
```

Plus, per the repo's file-split convention:
- `app_scripts.go` — Wails-bound `App` methods (list, enable, reload, grant, errors).
- Frontend `frontend/src/components/scripts/` — Scripts panel + permission prompt; backed by
  the shared status types, so a future unified "Extensions" view is cheap.
- `frontend/src/stores/scripts.ts` — Zustand store for script state.

**Shared, not merged.** Plugins bring their own runtime; scripts borrow ours. The core unifies
what they have in common *without* forcing scripting through the IPC/serialization path that
would dissolve its reason to exist (Runtime model, above).

**Migration discipline.** Plugins keep working throughout. They adopt the core *incrementally* —
the metadata registry is the natural first promotion (both need it), then trust/quarantine,
then the event router — never a big-bang rewrite. Scripts are built on the core from day one,
which is what proves the abstractions are real rather than speculative.

### Why the `cascade` sub-package is load-bearing

Yaegi exposes to a script *only* the symbols you inject via `interp.Use(...)`. Those symbols
are produced by `yaegi extract` over a Go package and committed as `symbols.go` — exactly
analogous to your committed Wails bindings (`frontend/wailsjs/`). **The set of injected
symbols *is* the sandbox.** A restricted script that writes `import "os"` fails to compile
because the symbol `os` does not exist in its universe. No syscall interception, no seccomp —
absence is the sandbox.

## What a script looks like

```go
// cascade:name greeter
// cascade:description Greets people who say hello, with a weather lookup.
// cascade:permissions storage, network
package main

import "cascade"

// --- Convention path: the exported name IS the hook. ---
func OnText(e cascade.TextEvent) {
    if e.HasPrefix("!hello") {
        e.Reply("Hi " + e.Nick)
    }
}

// --- Setup path (optional): timers, filters, multiple handlers, dynamic wiring. ---
func Setup(c *cascade.Client) {
    // Filtered subscription — only fires for matching messages.
    c.OnText(func(e cascade.TextEvent) {
        city := e.Arg(1)
        if cached, ok := c.Store.Get("weather:" + city); ok {
            e.Reply(cached)
            return
        }
        // `network` permission required for this to be in the symbol table.
        body, err := cascade.HTTPGet("https://wttr.in/" + city + "?format=3")
        if err != nil {
            e.Reply("weather lookup failed")
            return
        }
        c.Store.Set("weather:"+city, body)
        e.Reply(body)
    }, cascade.Match("!weather "))

    // Timers fall out of the registration API naturally.
    c.Every("10m", func() { c.Store.Sweep() })
}
```

Both paths are optional. A script may define only `OnText`, only `Setup`, or both. The
manager wires whatever is present.

## Script unit & authoring

### A script is a directory (a Go package)

The unit of a script is a **directory**, which Go already understands as a **package**. A
trivial script is a one-file directory; a complex one adds more files in the same package
(shared scope, no inter-file imports). The scripts root is a single **Go module**:

```
~/.cascade-chat/scripts/      ← module root: ONE go.mod; `cascade` resolvable here
  go.mod                      ← `module cascade-scripts` + a require/replace for the cascade pkg
  greeter/                    ← a script = a package; the directory name IS the script id
    greeter.go                ← `// cascade:` manifest header lives here (package main)
    weather.go                ← more files, same package, shared scope — the "complex" case
  logger/
    logger.go                 ← a "simple" script: a one-file directory
```

- Each script is `package main`; the host looks up `main.OnText` / `main.Setup`. Multiple
  `main` packages in one module is fine (it's the `cmd/*` pattern).
- **Load = type-check the whole package:** the engine reads *every* `.go` file in the script
  directory and compiles them as one unit. Capability is unchanged — a package gets one
  interpreter instance and one injected symbol table, so multi-file adds no sandbox surface.
- Identity, per-script KV, data dir, and the quarantine marker all key on the **directory
  name** — cleaner than per-file.
- Sub-packages *within* a script (a `greeter/util/` the script imports) are a later allowance;
  v1 targets the flat single-package case (see the Yaegi spike in Phasing/v1).

### Authoring happens in the user's own editor — there is no in-app editor

Because a script is real Go in a real module, the user's existing editor + **gopls** gives
full IntelliSense, hover types, go-to-def, and inline diagnostics — strictly better than any
editor we could embed, at ~zero build cost to us.

`The one enabling detail:` gopls only lights up if `import "<cascade module path>"` resolves
to actual source. So the scripts root must be **scaffolded as a well-formed module** — a
`go.mod` plus the `cascade` package resolvable (vendored, or a published module path with a
`replace`). That `go.mod` is what converts "edit in your own editor" from a hollow promise
into the real IDE. The *same* import path is read two ways: gopls reads it from the module,
Yaegi reads it from extracted symbols — high agreement, with **Yaegi-at-load staying
authoritative** (the Save→compile result is truth; editor diagnostics are advisory).

The app's only authoring affordance is a **"New script" scaffolder**: one action creates the
directory + a starter file (manifest header + skeleton) inside the module and reveals it. No
editing surface — pure onboarding.

## The `cascade` API surface

The typed façade. Everything below is concrete Go injected into the script's symbol table.
Restricted scripts get the **baseline** subset; permissions widen the table (§5.1).

### Client & registration (baseline)
```go
type Client struct { Store ScriptStore /* + internal handle */ }

func (c *Client) OnText(fn func(TextEvent), filters ...Filter)   // also OnJoin, OnPart, OnNotice, OnKick, OnNick, OnMode, OnConnect, OnDisconnect
func (c *Client) Every(interval string, fn func())      // recurring timer ("5m", "30s")
func (c *Client) After(delay string, fn func())         // one-shot timer
func Match(prefix string) Filter                         // first-arg filter helper; also MatchRegex, MatchChannel, MatchNick
// Note: no generics in the surface (per Open Question #1) — each On* takes a concrete
// func(<Event>) plus optional Filters, so Yaegi's weakest area is never exercised.
```

### Event structs (baseline)
```go
type TextEvent struct {
    Network string      // network name
    Channel string      // "" for a direct message
    Nick    string      // sender
    Message string      // full message text
    Tags    map[string]string  // IRCv3 message tags (read-only)
}
func (e TextEvent) HasPrefix(s string) bool
func (e TextEvent) Arg(n int) string      // whitespace-split argument, 1-based
func (e TextEvent) IsDM() bool
func (e TextEvent) Reply(msg string)      // reply to origin (channel, or sender if DM)
func (e TextEvent) ReplyNotice(msg string)
```
`JoinEvent`, `PartEvent`, `KickEvent`, `NickEvent`, `ModeEvent`, etc. follow the same shape:
plain typed fields + an origin-aware `Reply`.

### Actions
| Call | Permission | Notes |
|------|------------|-------|
| `e.Reply`, `e.ReplyNotice`, `c.Say(target, msg)` | baseline | Routed through the *same* send path as a user typing (`app_commands.go`). |
| `c.Join(channel)`, `c.Part(channel)` | baseline (own session) | Limited to the network the script's event came from. |
| `c.Store.Get/Set/Delete/Sweep` | `storage` | Persistent per-script KV (§7). In-memory script vars are baseline. |
| `cascade.HTTPGet/HTTPPost` | `network` | Outbound HTTP only; no raw sockets. |
| `cascade.ReadFile`, `cascade.WriteFile` | `filesystem` | Scoped to a per-script data dir. Trusted tier only. |
| `c.Notify(title, body)`, `c.SetMetadata(...)` | `ui` | Native notifications + the existing plugin metadata registry. |
| `c.Exec(...)` | `exec` | **Trusted-only, off by default.** See §5.1 — likely deferred indefinitely; this is plugin territory. |

### Script-local state (baseline)
In-memory variables are ordinary Go — package-level vars persist for the script's lifetime
(reset on reload). This is the mIRC `%var` equivalent and needs no permission. Durable state
requires `storage`.

## Trust & permissions

These mechanics live in the shared `internal/extension/` core (`trust.go`), not in the script
host — both runtimes use them, and quarantine benefits an untrusted downloaded *plugin* as much
as a script. The script host's only addition is mapping a granted permission to a symbol set.

### 5.1 Capability dial — trust tier → symbol table

Permission categories (manifest `// cascade:permissions ...`):

| Permission | Grants (symbols added to the table) | Tier |
|------------|-------------------------------------|------|
| *(baseline)* | `cascade` core: events, `Reply`/`Say`, timers, own-session `Join`/`Part`, in-memory state | any |
| `storage` | `c.Store.*` (durable KV) | any (prompted if quarantined) |
| `network` | `cascade.HTTPGet/HTTPPost` | any (prompted if quarantined) |
| `ui` | `c.Notify`, `c.SetMetadata` | any (prompted if quarantined) |
| `filesystem` | `cascade.ReadFile/WriteFile` (scoped dir) | **trusted only** |
| `exec` | `c.Exec` | **trusted only, default-disabled** |

The grant flow maps one-to-one onto the symbol table: granting `network` means the engine
calls `interp.Use(networkSymbols)` for that script; denying it means those symbols are simply
absent and any reference fails to compile. **The manifest is the sandbox configuration, not
decoration.**

### 5.2 Resource dial — always-on watchdog (every tier, including trusted)

Independent of trust. A script *you* wrote can still `for {}` by accident.

- Each handler dispatch runs in its own goroutine carrying a `context.Context` with a
  deadline (default e.g. 2s; configurable).
- The dispatch wrapper `recover()`s panics → the script is flagged, the error surfaced in the
  Scripts panel, other scripts unaffected.
- **Honest limitation:** Go has no safe goroutine preemption. A tight `for {}` with no API
  calls cannot be force-killed. What we *can* do: detect the deadline overrun, stop
  dispatching *new* events to that script, mark it **runaway**, notify the user, and
  auto-disable after N strikes. The spinning goroutine leaks but is isolated (other handlers
  keep running under `GOMAXPROCS`). This is the documented ceiling; genuinely hostile code is
  the out-of-process plugin model's job.
- Memory is best-effort: no per-goroutine cap exists. We monitor and disable repeat
  offenders rather than promising a hard limit.

### 5.3 Provenance — how a script gets its tier

- **Local-authored** (a script directory under `~/.cascade-chat/scripts/`): trusted. Declared
  permissions are auto-granted on load (with the full set, including `filesystem`/`exec`,
  available).
- **Quarantined** (anything that *arrived* — downloaded, imported, shared): loads
  **restricted**. Its manifest permissions are *requested*, not granted; the user sees a
  prompt ("greeter wants: storage, network") and grants per-permission. `filesystem`/`exec`
  are unavailable to quarantined scripts regardless of request.
- The quarantine bit is a local marker (a sidecar/DB flag set on import), mirroring macOS
  Gatekeeper. No code signing in v1/v2.

## Script lifecycle

```
discover ──▶ parse manifest ──▶ determine tier (origin + grants)
   ▲              │                      │
   │ file-watch   ▼                      ▼
   │         build symbol table ──▶ Yaegi Compile()  ──(type error)──▶ ERROR state (shown in panel)
   │                                      │
   │                                 instantiate
   │                                      │
   │                         scan OnText.../call Setup(c)
   │                                      │
   │                            subscribe handlers to event bus
   │                                      │
 reload ◀──────────────────────────── LOADED/ENABLED
```

- **Discovery:** scan the scripts module for script *directories* (each immediate subdirectory
  with `.go` files) on startup; a file watcher on those directories gives mIRC-style hot reload
  — any file change in a script's package reloads that package.
- **Load = type-check the package:** read every `.go` file in the script directory and
  type-check them as one unit (`interp.Compile` over the package). A type error means the
  script enters an **error state** with a clear message and never subscribes — satisfying
  decision #1 ("fails at load, not at 2am"). One script's error never blocks others.
- **Reload:** tear down the script's bus subscriptions and timers, re-compile the package,
  re-subscribe. In-memory state resets (documented).
- **Enable/disable:** per-script, DB-backed, exactly like plugins.

## Data flow

```
IRC line ─▶ irc/client.go ─▶ events/bus.go (pub/sub) ─▶ script/manager dispatch
                                                              │
                                              watchdog goroutine (ctx deadline + recover)
                                                              │
                                                   handler runs (cascade API call)
                                                              │
                                   translated to host action via the SAME paths as app_commands.go
                                                              │
                                                   irc/client.go send  (Reply == user typing)
```

The key property: `cascade` actions do not get a privileged backdoor. `Reply` calls the same
send path a user's keystroke would, so scripts cannot do anything the user couldn't do
manually at the same trust tier.

## Persistence (SQLite additions)

Reusing the existing single-connection WAL SQLite store (`internal/storage/`). New tables via
`schema.sql` + queries, then `task sqlc-generate`:

- `script_state(script_id TEXT, enabled INTEGER, quarantined INTEGER, granted_perms TEXT)` —
  one row per known script.
- `script_kv(script_id TEXT, key TEXT, value TEXT, PRIMARY KEY(script_id, key))` — the
  `storage`-gated KV. Per-script namespaced; a script cannot read another's keys.

## Wails-bound API (`app_scripts.go`)

All on the `App` struct, `main` package, per convention. ISO strings for any timestamps
(no `time.Time` across the boundary).

```
ListScripts() []ScriptInfo                  // name, status, tier, perms, load error
EnableScript(id string) error
DisableScript(id string) error
ReloadScript(id string) error
GetScriptError(id string) string            // type-check / runtime error text
GrantPermission(id, perm string) error      // for quarantined scripts
RevokePermission(id, perm string) error
NewScript(name string) (string, error)       // scaffold a script dir + starter file; returns its path
OpenScriptsDir() error                       // reveal in file manager for editing in the user's own editor
```
Bindings regenerated with `wails3 generate bindings -clean=true` after these land.

## Frontend — the management & permissions surface

With authoring delegated to the user's editor, the app's primary script UI is **management,
not editing**: *which scripts load, and what they're allowed to do.* This surface is the most
important app-owned UX, and because it's a view over the shared `extension/` registry + trust
store, it is runtime-agnostic — plugins and scripts can share **one "Extensions" view** (same
identity/perms/status/enabled fields). Its responsibilities:

1. **Inventory & load control** — every discovered script with a status badge
   (loaded / disabled / **error** / **runaway**), enable/disable toggle, reload, "open scripts
   folder", and the **"New script"** scaffolder.
2. **Permission transparency & control** — per script: trust tier (local-trusted vs.
   quarantined), declared/requested permissions, what's actually *granted*, and grant/revoke
   controls. This is where the tiered-trust model becomes *visible and actionable* rather than
   a header comment nobody reads.
3. **Trust review** — a quarantined (imported) script surfaces a review prompt
   ("greeter wants `network`, `storage` — grant?") before anything elevated loads, reusing the
   visual language of plugin permissions.
4. **Health & observability** — the Yaegi load error (with line), panic/runaway state, and a
   per-script log/output pane (the watchdog/panic/script logs — mIRC's status-window
   equivalent). This is where the *editor's* error display moves to: the app owns "why isn't my
   script working / is one misbehaving"; the editor owns "help me write it."
5. **Provenance** — where each script came from, so trust is legible at a glance.

- **Components** in `frontend/src/components/scripts/`, built on shared status types so the
  unified Extensions view is cheap later.
- **Zustand store** (`stores/scripts.ts`): subscribes to script-state events, exposes selectors
  the panel consumes (matching the `useNetworkStore(s => s.x)` slice pattern).

## Build step: symbol extraction

Yaegi cannot see host types without extracted symbols. Add a generation step (Taskfile
target, e.g. `task script-symbols`) running `yaegi extract` over `internal/script/cascade`,
producing the committed `cascade/symbols.go`. Run it whenever the `cascade` API changes —
analogous to `task sqlc-generate` and the Wails bindings flow. CI checks the generated file
is up to date.

## Error handling

- **Type / compile error:** script → error state, message in panel, no subscription. Never
  fatal to the host or siblings.
- **Panic at runtime:** recovered in the dispatch wrapper; logged; surfaced in panel; script
  stays loaded (one bad event doesn't kill it) but repeated panics auto-disable.
- **Deadline overrun / runaway:** stop dispatching, mark runaway, notify, auto-disable after
  N strikes (§5.2).
- **Permission-absent reference:** caught at compile time (symbol missing) — surfaces as a
  load error telling the user which permission to add.

## Testing strategy

Per `CLAUDE.md` (Go tests need `-tags fts5`, `CGO_ENABLED=1`):
- **Engine:** load a script that type-errors → assert error state, no subscription. Load a
  valid script → assert handler fires on a synthetic bus event.
- **Multi-file package:** load a script *directory* whose handler in `a.go` calls a helper in
  `b.go` → assert it type-checks and runs as one package (the Yaegi spike, made permanent).
- **Sandbox/capability:** a restricted script referencing `os`/`net` → assert compile failure;
  granting `network` → assert `HTTPGet` resolves.
- **Watchdog/resource:** a handler that sleeps past the deadline → assert runaway flagged and
  dispatch stops; a panicking handler → assert recovery + sibling unaffected.
- **Trust/provenance:** quarantined script → assert loads restricted and perms are requested
  not granted; local script → assert auto-grant.
- **Store:** per-script namespacing — script A cannot read script B's keys.
- **Frontend:** vitest for the panel/store (enable/disable/error rendering, permission modal).

## Phasing

The full tiered-trust **C** is the destination, but it lands in two cuts so the
Yaegi-integration and safety model are proven before the trust apparatus is built.

### v1 — the engine (on the shared core)
Stand up `internal/extension/` with the slice scripts need — identity/registry, the event
router + `Host.Deliver` seam, command-registration binding — and build the Yaegi **script**
host on it: **scripts-as-directories** in a scaffolded **Go module** (so external gopls works),
local **trusted** scripts only, convention + `Setup`, baseline capabilities (messaging, timers,
own-session join/part, in-memory state), static type-check at load, hot reload, enable/disable,
watchdog + panic recovery, the **management surface** (inventory, enable/disable, errors, "New
script" scaffolder — no permission UI yet, no in-app editor). Plugins are otherwise untouched;
the one shared-core change they may take opportunistically is the **metadata registry**
promotion, a clean lift both use. *No* quarantine, *no* `storage`/`network`/`filesystem`/`exec`.

**v1 proves:** Yaegi compile/type-check + symbol extraction, **multi-file package loading**
(the directory-as-package spike — verify before committing the UX to it), module scaffolding +
gopls resolution, the event-router abstraction, the resource watchdog, the lifecycle/reload UX,
and that the shared-core seams hold up under a real second runtime.

### v2 — the trust apparatus + plugin convergence
Quarantine bit + manifest permission requests + permission-prompt UI + elevated capabilities
(`storage`, `network`, `ui`, and `filesystem` for trusted), all landing in `extension/trust.go`.
The **plugin host migrates onto the shared permission + event-router core** here, so both
runtimes share one trust model and one Extensions listing. This is the full **C**.

### v3 (future, out of scope here)
- **Shared-sidecar script host.** If accidental runaways prove painful in practice (§5.2), move
  Yaegi into a single script-host subprocess the client can `Kill()`+restart, reloading the
  survivors — one runaway becomes a brief blip for all scripts rather than an in-process leak.
  Because the sidecar is *just another `extension.Host`* and the `cascade` façade is unchanged,
  this is a host swap: no core change, no script rewrites. The shared plumbing is precisely what
  makes it cheap — this is the payoff of designing for the core now.
- **Sharing/import flow,** a script directory/registry, optional code signing, `exec`
  reconsidered.

## Open questions / risks

1. **Yaegi durability.** Its development has slowed and generics support has lagged. Mitigation:
   keep the `cascade` surface generics-free so scripts stay portable even if the interpreter
   is ever swapped. **Risk accepted** — the only escape is real compilation, which is the
   plugin model.
2. **`exec` — keep it at all?** Strong argument to drop it entirely: anything wanting to shell
   out should be a plugin. Leaning **defer/drop**; recorded as v3-or-never.
3. **Default deadline value.** 2s is a guess; tune against real handlers (some legitimately do
   network I/O under `network`). May need per-script override.
4. **Hot-reload + in-memory state.** Reload resets script vars. Acceptable and documented, but
   worth a UX note so users aren't surprised.
5. **Plugin-migration blast radius.** Converging the *working* plugin system onto the shared
   core (v2) touches stable code. Mitigation: incremental adoption (metadata → trust → router),
   each step behind tests, with the plugin JSON-RPC path otherwise untouched. v1 deliberately
   leaves plugins alone so the core is validated by scripts first.
