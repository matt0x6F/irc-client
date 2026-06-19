# Command Registry, Autocomplete, Categorized Help & DM-on-double-click — Design

> **Status:** Design (approved). Implementation plan (task-by-task) to be generated from this spec via superpowers:writing-plans, then executed with superpowers:subagent-driven-development.

**Goal:** Give Cascade Chat a real command system: a single source-of-truth command registry that *drives dispatch* (not just metadata), slash-command autocomplete (popup + inline argument hint), an application help surface that **separates client / server / CTCP / plugin commands**, the ability for **plugins to register their own slash commands**, and DM-on-double-click of a user in the nick list (with `/query` as the discoverable, navigation-correct equivalent).

**Tech Stack:** Go (`ergochat/irc-go`, Wails v3, JSON-RPC plugin subprocesses), React 19 + Zustand + TypeScript, Vitest. Backend tests use the `fts5` build tag.

---

## Motivation / current state

- **Commands**: `SendCommand` in [app_commands.go:56](../../app_commands.go) is a ~30-case `switch`. Known commands are parsed/validated inline; **unknown `/commands` fall through to raw passthrough** (`client.SendRawCommand`). Usage strings exist *only* inside error messages. There is **no registry, no metadata, no `/help`, no command autocomplete**.
- **Frontend**: [network.ts `sendMessage`](../../frontend/src/stores/network.ts) only special-cases `/me`, `/part`, `/leave` (target injection), then forwards everything to the backend. [input-area.tsx](../../frontend/src/components/input-area.tsx) Tab-completes **nicks only**.
- **DMs**: `/query` / `/msg` already exist backend-side and open a PM conversation (`GetOrCreatePMConversation` + `SetPrivateMessageOpen`), but the frontend does not reliably **navigate** to the new pane. PM panes are addressed `"pm:<nick>"` and opened via `selectPane(networkId, "pm:<nick>")`.
- **Nick list**: [channel-info.tsx:540](../../frontend/src/components/channel-info.tsx) has a right-click context menu only — **no click/double-click handler**.
- **Plugins**: subprocess + JSON-RPC. In their `initialize` response they declare `Events` and `MetadataTypes` ([protocol.go:36](../../internal/plugin/protocol.go)). A `Permissions` field is defined-but-unused. Plugins receive events read-only. An `actionQueue` + `processPluginActions` ([app_events.go:225](../../app_events.go)) exists for a `send_message` action but is **not wired** to inbound plugin notifications. **No command concept exists.**

---

## A. Command registry (Go) — single source of truth, drives dispatch

A `CommandSpec` carries both behavior and metadata:

```go
type Category string

const (
    CategoryClient Category = "client" // client interprets it (query, close, help, clear, ignore)
    CategoryServer Category = "server" // IRC protocol verb (join, part, nick, mode, whois, topic, kick…)
    CategoryCTCP   Category = "ctcp"   // ctcp, version, time, ping, clientinfo
    CategoryPlugin Category = "plugin" // registered by a plugin
)

type HandlerFunc func(a *App, networkID int64, args []string, raw string) error

type CommandSpec struct {
    Name        string      // canonical, upper-case: "JOIN"
    Aliases     []string    // ["J"]
    Category    Category
    Usage       string      // "#channel [key]"  (argument syntax only, no leading command)
    Description string      // one line
    MinArgs     int         // emit a uniform usage error when unmet
    Source      string      // "" for built-in; plugin name for plugin commands
    Frontend    bool        // true => intercepted/handled in the frontend (e.g. /help, /clear)
    handler     HandlerFunc // built-ins only; nil for plugin/frontend commands
}
```

**Registry**: a `CommandRegistry` builds `map[string]*CommandSpec` keyed by `Name` **and** every alias (upper-cased). Built-in specs are registered at startup. The 30-case switch body is refactored into one `HandlerFunc` per command (behavior-preserving — same logic, new shape), kept in `app_commands.go` / a new `commands_registry.go`.

**Dispatch** (`SendCommand`): trim → if `/`-prefixed, split → look up by upper-cased verb:
1. **Built-in** spec with handler → validate `MinArgs` (uniform `usage: /<name> <usage>` error) → call handler.
2. **Plugin** spec → invoke via plugin path (section B).
3. **Not found** → `client.SendRawCommand(command[1:])` — **unchanged raw passthrough** (load-bearing: keeps the client working against server-extension commands we don't model).
Non-`/` input → `client.SendRawCommand(command)` (unchanged).

**Categorization** is data on each spec. CTCP is its own group (collapsible into Server later if desired).

**Wails surface**: new bound `GetCommands() []CommandSpec` returning the merged registry **metadata** (no handlers over the wire) for the frontend. Requires a binding regen (`wails3 generate bindings -clean=true`).

### Invariants
- Raw passthrough for unknown commands MUST be preserved.
- The registry refactor of the existing switch MUST NOT change observable dispatch behavior (asserted by tests) before any new feature rides on it.

---

## B. Plugin-provided slash commands

- **Protocol**: `InitializeResult` ([protocol.go:36](../../internal/plugin/protocol.go)) gains `Commands []CommandSpecWire` (`name, aliases, usage, description` — no handler over the wire).
- **Registration**: the plugin `Manager` collects declared commands into a plugin-command table; the `App` merges built-ins + plugin commands when answering `GetCommands()` and when dispatching.
- **Conflict policy**:
  - Plugins **cannot shadow built-ins** — built-in lookup wins; a plugin command colliding with a built-in name/alias is rejected with a logged warning.
  - **Plugin-vs-plugin**: first registration wins; later collisions rejected + logged (mirrors the existing `Priority` precedent in the metadata system).
- **Invocation**: new host→plugin RPC `command.invoke` with `{command, args, networkID, channel}`. The plugin performs its work and replies by emitting a host action (e.g. a `message.send` notification), which requires **wiring the dormant `actionQueue` / `processPluginActions`** ([app_events.go:225](../../app_events.go)) to a new plugin→host action notification handler in [ipc.go](../../internal/plugin/ipc.go) (today only `ui_metadata.set` is handled). This activates the existing half-built return path.
- **Refresh**: when plugins load/unload, the frontend refetches `GetCommands()` so autocomplete/help reflect plugin commands.

---

## C. Frontend — autocomplete, help, DM

### Registry consumption
- The network store fetches `GetCommands()` on startup and caches it; refetch on plugin load/unload.

### Autocomplete ([input-area.tsx](../../frontend/src/components/input-area.tsx))
- Typing `/` **at line start** opens a popup of matching commands (name + `Usage`), filtered as you type. `↑/↓` to move, `Tab`/`Enter` to accept, `Esc` to dismiss.
- After the command name + space, render a **ghosted inline argument hint** from the spec `Usage` (e.g. `#channel [key]` after `/join `).
- Existing **nick Tab-completion is preserved** for mid-line / non-command context. (At line start with a leading `/`, Tab drives the command popup; otherwise Tab completes nicks.)

### Help — "Both" (buffer + panel)
- **`/help <cmd>`**: prints that command's `usage` + `description` as system message(s) into the current buffer (classic, scrollable).
- **`/help`** (no args): opens a **searchable Help panel** (modal) with sections **Client / Server / CTCP / Plugin**, each row showing usage + description; plugin commands grouped by owning plugin. Also openable via menu/keybind.
- Help is **handled frontend-side** (it owns the cached registry) using the same interception pattern as `/me`/`/part` today. The Go registry still carries a `/help` spec (`Category: client`, `Frontend: true`) so it appears in autocomplete and the panel.

### DM on double-click + `/query` navigation
- `onDoubleClick` on a nick in [channel-info.tsx:540](../../frontend/src/components/channel-info.tsx) calls a new store action `openQuery(networkId, nick)` → ensure conversation open (backend) → `selectPane(networkId, "pm:"+nick)`.
- **`/query` (and double-click) navigate** to the DM pane after opening. **`/msg` stays a one-off send** that does not switch panes. This delivers "double-click → DM" + "a slash command for one" (`/query`), now discoverable via autocomplete/help and navigation-correct.

---

## D. Phasing, testing, docs

Phased so each lands independently green:

1. **Go registry + dispatch refactor** (behavior-preserving) + `GetCommands()` binding + regen.
2. **Frontend autocomplete** (popup + inline hint) consuming the registry.
3. **Help** (`/help <cmd>` → buffer; `/help` → panel) + **DM double-click** + **`/query` navigation**.
4. **Plugin command registration** (protocol `Commands`, manager registration + conflict policy, `command.invoke` RPC, action-queue wiring) + docs.

### Testing
- **Go** (`CGO_ENABLED=1 go test -tags fts5 ./... -count=1`): registry lookup, alias resolution, category assignment, dispatch routing (built-in vs passthrough), `MinArgs` usage errors, plugin command registration + conflict rejection (built-in shadow, plugin-vs-plugin), `command.invoke` marshaling + action-queue round-trip.
- **Frontend** (`cd frontend && npx vitest run`): command parsing + autocomplete filtering, inline hint derivation, help panel rendering + section grouping, `/help <cmd>` buffer output, `openQuery`/double-click navigation, `/msg` does-not-switch.
- **e2e screenshots** (per quality bar — visual surfaces only): autocomplete popup, Help panel.

### Docs
- Update [docs/plugin-system.md](../../docs/plugin-system.md): new `Commands` field in `initialize`, `command.invoke` RPC, the plugin→host action notification contract, and conflict policy.
- Document the command list / `/help` for users.

---

## Out of scope (YAGNI)
- Replacing raw passthrough or modeling every server-extension command.
- Per-command permission gating for plugins (the `Permissions` field stays unused here).
- Rebindable keyboard shortcuts for commands.
