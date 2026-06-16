# Plan: MCP Read Surface for Cascade

**Status:** Draft
**Date:** 2026-06-16
**Owner:** Matt

## Goal

Expose an embedded, localhost-only MCP server from the Cascade binary so an MCP
client (Claude Desktop, Claude Code, a user-run agent) can **read** the user's
chat: list networks/channels, read recent buffers, and run rich search over
local history. This is "Product A" — an AI copilot for *your* chat, operating
through your already-authenticated, already-running session.

**Read-only in V1.** No tool can send a message or mutate state. The surface
literally has no write path. This is the primary safety property; sending is a
separate, later, deliberately-gated project.

## Why embedded (not a standalone MCP server)

The value is reuse of the running GUI's live state: authenticated IRC sessions,
warm in-memory network/channel state, and the local SQLite history. A
standalone server would just reconnect to Ergo as another client and have none
of that. This also dictates the transport (below).

## Key decisions (settled during brainstorming)

1. **Transport:** localhost-bound streamable-HTTP/SSE listener inside the
   running app. NOT stdio — a spawned stdio subprocess would be cold (no live
   connections, fighting the single WAL SQLite connection). MCP clients connect
   *into* the live process.
2. **Off by default.** Enabled via Settings. Bound to `127.0.0.1` only.
   Bearer-token auth; token stored in the `settings` table and surfaced in the
   Settings UI for the user to copy into their client config.
3. **Read-only tool surface.** `list_networks`, `list_channels`, `read_buffer`,
   `search`.
4. **Search = lexical + filters + context expansion.** Semantic/embeddings is
   **deferred** to its own future round. The `search` tool contract is designed
   so a semantic/hybrid backend can slot underneath later without changing what
   the agent sees.

## Architecture

New isolated package `internal/mcp/`. It receives a **narrow read-only
interface**, never the whole `*App`:

```go
// internal/mcp/reader.go
type ChatReader interface {
    ListNetworks(ctx context.Context) ([]NetworkInfo, error)
    ListChannels(ctx context.Context, networkID int64) ([]ChannelInfo, error)
    ReadBuffer(ctx context.Context, networkID int64, target string, limit int, beforeISO string) ([]Message, error)
    Search(ctx context.Context, q SearchQuery) ([]SearchResult, error)
}
```

- The concrete implementation lives in `main` (or a thin adapter) and is backed
  by `*storage.Storage` (already on `App.storage`) plus the in-memory network
  registry for live connection status. Reads reuse existing methods where they
  exist (e.g. `storage.GetMessages(networkID, *channelID, limit)`).
- Because the interface exposes no writer, the MCP layer *cannot* send even by
  accident — enforced by the type system, not discipline.

### Lifecycle / wiring
- `internal/mcp/server.go`: builds the MCP server, registers tools, owns the
  HTTP listener. `Start(ctx)` / `Stop(ctx)`.
- In `App` startup: if `mcp_enabled`, construct the reader adapter + server,
  bind `127.0.0.1:<mcp_port>`, start. Stop on app shutdown (hook into existing
  `shutdownOnce` path).
- Confirm the current Go MCP SDK + its streamable-HTTP transport API during
  implementation (check the official `modelcontextprotocol/go-sdk`; use
  context7 for current docs). Do not hardcode an SDK assumption from this plan.

## Tool surface (all read-only)

| Tool | Args | Returns |
|---|---|---|
| `list_networks` | — | networks + connection status |
| `list_channels` | `network_id` | joined channels / query buffers |
| `read_buffer` | `network_id`, `target`, `limit`, `before?` | recent messages (reuses history path) |
| `search` | `query`, `filters`, `limit`, `context` | ranked hits, each with a surrounding conversation window |

### `search` contract (the centerpiece)

```
search(
  query:   string            // FTS5 query
  filters: {
    networks?:    int64[]
    channels?:    string[]
    from?:        string      // author nick
    since?:       string      // ISO
    until?:       string      // ISO
    mentions_me?: bool
    has_link?:    bool
    is_pinned?:   bool
  }
  limit:   int = 20
  context: int = 4            // messages before/after each hit
)
```

- **Matching:** FTS5 `MATCH` over the existing `messages_fts` index, ranked by
  `bm25()`. The index + sync triggers already exist in `schema.sql`; no query is
  wired to them yet.
- **Filters:** composed as SQL `WHERE` against `messages`
  (`network_id`, `channel_id`, `timestamp`, `user`, plus `is_pinned` via
  `pinned_messages`). `mentions_me` / `has_link` are predicates on message text.
- **Context expansion:** for each hit, fetch `context` messages before/after in
  the same channel by id/timestamp; merge overlapping windows into a single
  conversational chunk so the agent reads exchanges, not orphaned lines. This is
  the anti-Slack feature and the main reason agent-driven search beats a results
  list.
- **SQLC caveat:** FTS5 + dynamic optional filters fights SQLC's static-SQL
  model. Expect a hand-written query/builder in the storage layer for search,
  rather than a generated SQLC method. Keep it in `internal/storage/` next to
  the other queries.

## Settings

Three keys in the existing `settings` table:
- `mcp_enabled` (bool, default false)
- `mcp_port` (int, default e.g. 8765)
- `mcp_token` (string, generated on first enable)

Settings UI: a toggle, a port field, and a read-only token field with a copy
button + a short "paste this into your MCP client" hint.

## Implementation phases

Build storage-up, test at each layer, wire the protocol last.

1. **Search query in storage (TDD).**
   - Add a hand-written search method to `internal/storage/` taking a struct of
     optional filters + query, returning hits with rank.
   - Tests (with `fts5` build tag): each filter in isolation, combinations,
     empty query, no-match, ranking order. Seed a temp SQLite.
2. **Context expansion (TDD).**
   - Window fetch + overlap-merge, in storage or `internal/mcp/`.
   - Tests: adjacent hits merge; non-adjacent stay separate; window clipped at
     buffer edges.
3. **Reader adapter + the other three read methods.**
   - `ChatReader` impl backed by `storage` + network registry.
   - Reuse `GetMessages` etc. Tests against seeded storage.
4. **MCP server + tools.**
   - Register the four tools against the chosen Go MCP SDK; streamable-HTTP
     transport bound to `127.0.0.1`; bearer-token middleware.
   - Protocol-level test: `initialize` → `tools/list` → `tools/call` for each
     tool against a temp DB.
5. **Settings + lifecycle wiring.**
   - `mcp_enabled/port/token` read on startup; start/stop server; token
     generation on first enable.
   - Settings UI controls.
6. **Docs.**
   - Short `docs/mcp-server.md`: how to enable, where the token is, example
     client config, the read-only guarantee.

## Testing

- Go: `CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1` (fts5 tag is
  mandatory for the search tests).
- Frontend: vitest for the Settings controls.
- `task check` before done.

## Out of scope (explicitly)

- **Any write/send tool.** Separate future project, will be human-gated.
- **Semantic / embedding search.** Deferred; contract is forward-compatible.
- **Remote / non-localhost access, multi-user.** Localhost only.
- **MCP resources/prompts.** Tools-only for V1; revisit later.
- **Agent-to-agent coordination ("Product B").** Different architecture
  (agents as IRC identities), not this binary's MCP surface.

## Open questions to confirm during implementation

- Which Go MCP SDK + exact streamable-HTTP transport API (verify against
  current docs).
- `mentions_me` definition: match the connected nick(s) per network — confirm
  how the current nick is tracked in the network registry.
- Default port choice and collision handling if the port is busy.
