# Full-Stack E2E Testing — Design

**Date:** 2026-06-13
**Status:** Approved (design); spike complete
**Scope:** Automated, worktree-isolated, full-stack end-to-end tests for Cascade Chat, runnable locally with one command and in GitHub Actions CI.

## 1. Goal

Drive the real React UI through the real Wails backend, real `IRCClient`, and a real
containerized Ergo IRC server — with no human clicking. Every run is isolated per
worktree so multiple worktrees (and the developer's real app) can run concurrently
without colliding on data, container names, or ports.

This is **Approach 1** (orchestrate `wails dev` + Playwright). Rejected alternatives:
- *Drive the built native binary* — webview remote-debug is platform-specific and flaky.
- *Headless binding server* — a second entrypoint that must mirror Wails' binding surface;
  kept as a possible future CI hardening, not built now.

## 2. Architecture

```
Playwright (Chromium, headless)
        │  HTTP/WS  →  http://localhost:<BRIDGE_PORT>     (wails dev -devserver)
        ▼
   wails dev process  ── runs its own Vite watcher @ <VITE_PORT>, serves window.go bindings
        │  in-process
        ▼
   App (Go, built with -tags fts5) → IRCClient → tcp localhost:<ERGO_PORT>
                                                        │
                                       Ergo container (docker compose -p cascade-e2e-<id>)
```

All four of `<BRIDGE_PORT>`, `<VITE_PORT>`, `<ERGO_PORT>`, and the data directory are
allocated/created per run.

## 3. Isolation contract (worktree parallelism)

| Resource            | Today (global, collides)          | Per-run                                             |
|---------------------|-----------------------------------|-----------------------------------------------------|
| SQLite DB + plugins | `~/.cascade-chat/`                | `CASCADE_DATA_DIR=<tmp>` env override               |
| Ergo container      | `container_name: ergo-irc-test`   | compose project `cascade-e2e-<id>`, no fixed name   |
| Ergo ports          | `6667:6667` / `6697:6697`         | `${ERGO_PORT}:6667` (free port, host side)          |
| Vite                | `5173`                            | `<VITE_PORT>` (free, via `VITE_PORT` env)           |
| Wails bridge        | `34115`                           | `<BRIDGE_PORT>` (free, via `-devserver`)            |

`<id>` and all ports are obtained by allocating free TCP ports at startup (robust against
hash collisions), not derived from the worktree path.

## 4. Production code change (minimal)

`app.go` `NewApp()`: resolve a single `baseDir` from `CASCADE_DATA_DIR`, falling back to
`~/.cascade-chat` when unset; route both the DB path and plugin dir through it.
**Status: implemented and verified** (`CGO_ENABLED=1 go build -tags fts5 .` passes).
This is the only production-code change, and it independently protects the developer's
real chat history from being clobbered when running the app out of a worktree.

## 5. Spike findings (validated empirically, Wails v2.10.2)

1. **`-tags fts5` is mandatory.** Plain `wails dev` compiles the SQLite driver without
   FTS5; the app crashes at startup on the `messages_fts` migration
   (`no such module: fts5`) before serving anything. The harness must run
   `wails dev -tags fts5`.
2. **Drive Vite via `VITE_PORT`, not `-frontenddevserverurl`.** Passing an external
   frontend URL makes Wails *also* spawn its own watcher → two Vite servers, the second on
   a fixed port (breaks parallelism). Instead: let Wails run its own watcher and make
   `vite.config.ts` read `process.env.VITE_PORT`. Wails auto-detects the URL from the
   watcher's stdout (`Vite Server URL: http://localhost:<port>/`). Single, dynamic Vite.
3. **Bindings reachable over the bridge.** With the tag, the app boots, the bridge serves
   HTTP 200, and `/wails/ipc.js` (the binding transport) is served — so `window.go.*`
   calls work from an external headless browser.
4. **`wails dev` must run from the project root** (where `wails.json` lives).

## 6. Orchestration

Playwright `globalSetup` / `globalTeardown` (TypeScript — same code path locally and in CI):

1. Allocate free ports (`BRIDGE_PORT`, `VITE_PORT`, `ERGO_PORT`); create a temp data dir;
   derive `<id>`.
2. `docker compose -p cascade-e2e-<id> up -d` with `ERGO_PORT` exported; wait until Ergo
   accepts a TCP connection on `ERGO_PORT`.
3. Spawn `wails dev -tags fts5 -devserver localhost:$BRIDGE_PORT` from the repo root, with
   `CASCADE_DATA_DIR`, `VITE_PORT` in env. Export `ERGO_PORT` to the test process so specs
   can read it.
4. Poll `http://localhost:$BRIDGE_PORT/wails/ipc.js` until 200; run specs.
5. Teardown (always): kill the `wails dev` process tree, `docker compose -p ... down -v`,
   remove the temp data dir.

Wrapped in a `task e2e` target for one-command local runs.

## 7. Docker changes

`docker-compose.yml`:
- Remove `container_name` (a fixed name makes two compose projects collide).
- Make host ports env-interpolated with sensible defaults so manual `docker compose up`
  still works as documented: `"${ERGO_PORT:-6667}:6667"`, `"${ERGO_TLS_PORT:-6697}:6697"`.

## 8. Starter test scenarios

1. **Connect** — drive the real add-server form (host `localhost`, port from
   `process.env.ERGO_PORT`); assert registration completes (welcome/status line renders).
2. **Join** — `/join #test`; assert the channel buffer and nick list appear.
3. **Send & persist** — send a message; assert it renders (echo-message) and survives a
   page reload (DB persistence through the temp DB).
4. **Two-party chat** — a lightweight second IRC peer (a raw `ergochat/irc-go` connection
   in the harness, not a second UI) joins `#test` and speaks; assert the message appears
   in the UI.

## 9. CI (GitHub Actions, Linux)

Install webkit2gtk + Go + Node + Playwright browsers; run the suite under `xvfb-run` so the
native WebKit window renders invisibly while Playwright drives Chromium headless against the
bridge. Same `globalSetup` as local. The `-tags fts5` build requires `CGO_ENABLED=1` and a C
toolchain on the runner.

## 10. Out of scope

- Driving the production native binary directly.
- Concurrent assertions across multiple *UI users* in one run (the two-party scenario uses a
  raw IRC peer, not a second app instance).
- The headless binding server (Approach 3) — left as a clean future option if CI proves
  painful under xvfb.
