# Cascade Chat — Development Guide

## Project Overview

Modern multi-platform IRC client built with Wails (Go backend + React frontend).

## Quick Commands

```bash
task dev              # Dev server with hot reload
task dev:mcp          # Dev + built-in Wails MCP server (LLM drives the live native app)
task build            # Build for current platform
task check            # Run all checks (fmt, lint, test, type-check)
task test             # Go tests
task sqlc-generate    # Regenerate SQLC code after schema/query changes
```

> **Driving the live app (`task dev:mcp`):** sets `WAILS_MCP=1` so Wails compiles
> in its experimental MCP server (loopback `:9099`). Connect with
> `claude mcp add --transport http cascade-dev http://127.0.0.1:9099/mcp`, then an
> agent can inspect the DOM, call bound Go methods, await events, and simulate
> input against the **real native window** — the gap the server-mode Playwright
> e2e suite can't reach. Playwright stays the deterministic CI regression gate;
> this is a local dev/verification loop. The `mcp` tag is never in production builds.

## Architecture

- **Backend (Go):** `app.go` + `app_commands.go` + `app_connection.go` + `app_events.go` — Wails-bound methods split by responsibility
- **IRC Core:** `internal/irc/client.go` — Connection management, SASL, CAP negotiation
- **Storage:** `internal/storage/` — SQLite with WAL mode, SQLC-generated queries, buffered writes
- **Events:** `internal/events/bus.go` — Pub/sub event bus with wildcard support
- **Plugins:** `internal/plugin/` — JSON-RPC 2.0 over stdin/stdout, subprocess model
- **Frontend:** `frontend/src/` — React 19, TypeScript, ShadCN UI, Tailwind CSS v4
- **State:** `frontend/src/stores/` — Zustand stores (network.ts, ui.ts)

## Code Conventions

### Go
- All Wails-bound methods must be on the `App` struct in `main` package
- Split by file: `app.go` (core/CRUD), `app_commands.go` (IRC commands), `app_connection.go` (connection lifecycle), `app_events.go` (event routing)
- Error handling: always return errors, wrap with `%w`
- Concurrency: mutexes for shared state in App struct
- Database changes: edit `internal/storage/schema.sql` and `internal/storage/queries/`, then run `task sqlc-generate`

### TypeScript/React
- Functional components with hooks
- State management via Zustand stores in `frontend/src/stores/`
- Components subscribe to specific store slices: `useNetworkStore(s => s.selectedNetwork)`
- ShadCN UI components in `frontend/src/components/ui/`

## Testing

```bash
# Backend (fts5 tag required for SQLite full-text search)
CGO_ENABLED=1 go test -tags fts5 ./internal/... -count=1

# Frontend
cd frontend && npx vitest run
```

## Database

- SQLite with WAL mode, single connection
- Schema: `internal/storage/schema.sql`
- Queries: `internal/storage/queries/`
- SQLC config: `sqlc.yaml`
- Location: `~/.cascade-chat/cascade-chat.db`

## Plugin Development

- Plugins are executables using JSON-RPC 2.0 over stdin/stdout
- Discovery: `~/.cascade-chat/plugins/` directory or `cascade-*` in PATH
- See `docs/public/developers/plugin-system.md` for protocol details
- Example plugins in `plugins/` directory
