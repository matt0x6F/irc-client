# Plan: Files (DCC peer-to-peer transfers)

**Status:** Draft
**Date:** 2026-06-16
**Owner:** Matt

## Goal

Bidirectional peer-to-peer file transfer between IRC users, presented as a
clean **"Files"** interface — a transfer manager in the spirit of AirDrop /
Slack uploads. The underlying protocol is DCC (Direct Client-to-Client, CTCP-
negotiated), but **the word "DCC" must appear nowhere in the UI.** Users think
in files, peers, and progress; the protocol is plumbing.

Symmetric from day one: you can both **send** files to and **receive** files
from a peer (option 3).

## Why a clean "Files" layer

DCC is a 1990s plumbing detail. The mental model users already have is "a file
arrived — accept it — it lands somewhere — I can see progress and history." The
interface is a **transfer manager**: direction, peer, filename, size, progress,
speed, status, and actions (accept / decline / cancel / reveal). DCC negotiation
is an implementation detail behind it.

## Key decisions (settled during brainstorming)

1. **Symmetric V1** — send *and* receive, full transfer manager both directions.
2. **NAT strategy: prefer outbound, map when you must listen, flip when you
   can't.**
   - Always prefer being the side that connects *out* (outbound TCP traverses
     NAT; inbound is the problem).
   - When you must listen (active send, or receiving a passive offer),
     opportunistically obtain a port mapping via **UPnP / NAT-PMP**.
   - If you can't become reachable, fall back to **passive/reverse DCC** so the
     peer listens and you connect out.
3. **Scope matches classic DCC — no more.** If *both* peers are behind NAT and
   neither can get a port mapping, pure DCC cannot connect them (no hole-
   punching/relay in the protocol). This is **explicitly out of scope for V1**
   and surfaced as a clear "couldn't connect — both sides unreachable" state,
   never a silent hang. (A future relay / Ergo-side extension is the eventual
   answer, but not now.)
4. **Dedicated package.** Transfer machinery does NOT go into
   `internal/irc/client.go` — that file is already ~3,100 lines. DCC CTCP
   negotiation is *routed* from there into a new `internal/dcc/` package with a
   minimal hook.

## Architecture

### `internal/dcc/` (new package)
- **Negotiation** (`protocol.go`): parse/format the CTCP DCC messages —
  `DCC SEND <filename> <ip-uint32> <port> <filesize>`, the passive variant
  (`port = 0` + token), and `DCC RESUME` / `DCC ACCEPT`. IPv4 address is a
  big-endian uint32; note IPv6/literal handling as a consideration.
- **Transfer engine** (`transfer.go`): one goroutine per transfer owning its TCP
  socket. Receiver connects (or listens, passive), streams to a temp file, sends
  periodic 4-byte total-received ACKs (classic compat), then atomically renames
  into place. Sender listens (or connects, reverse) and streams bytes.
- **Listener + NAT** (`nat.go`): listener lifecycle, plus UPnP (IGD) / NAT-PMP
  discovery and port mapping; reachability detection; the active-vs-passive
  decision. (Pick a Go NAT lib during impl — e.g. `huin/goupnp`,
  `jackpal/go-nat-pmp`; verify current options.)
- **Manager** (`manager.go`): registry of transfers (offered / queued / active /
  paused / done / failed / cancelled), accept/decline/cancel, emits progress
  updates via the event bus.

### Hook into the IRC layer
- In `client.go` CTCP handling (around the existing `handleCTCPRequest` /
  CTCP-parse path near line 569/2902), route `DCC ...` CTCP messages to a
  callback into the dcc manager. Keep the `client.go` change *small* — detect
  and delegate, nothing more.

### App / Wails binding
- New `app_files.go` with read + action methods: `ListTransfers`,
  `AcceptTransfer`, `DeclineTransfer`, `CancelTransfer`, `SendFile`,
  `RevealTransfer`, `SetDownloadDir`. Progress streamed to the frontend via the
  event bus (no `time.Time` across the Wails boundary — ISO strings only).

### Storage
- New `transfers` table: id, network_id, peer, direction, filename, size,
  transferred, status, error, started_at, finished_at (timestamps as TEXT/ISO).
  Live progress is in-memory; final/historical records persist. Schema +
  queries via SQLC (`task sqlc-generate`).

### Frontend
- A dedicated **Files** view (transfer manager): list with per-row direction,
  peer, name, size, progress bar, speed, status, and contextual actions. An
  incoming offer raises a notification and appears as a pending row awaiting
  accept/decline. History persists across restarts.

## Security posture

- **Never auto-accept.** Every incoming transfer requires explicit user accept.
- **Filename sanitization.** Strip directory components and any path-traversal
  (`..`, absolute paths); collision-safe renaming in the download dir.
- **Sandboxed download directory** (setting; default e.g. `~/Downloads/Cascade`
  or `~/.cascade-chat/files`). Free-space / size check before accepting.
- **Surface peer + IP**, and note that any transfer reveals your IP to the peer
  (a real privacy property of DCC).
- **Listeners** bound to a configurable port (range), closed promptly when the
  transfer ends; mappings released.
- Warn on executable/suspicious extensions before saving.

## Settings

- `files_download_dir` (path)
- `files_listen_port` (or small range)
- `files_upnp_enabled` (bool, default true)
- `files_max_size` (optional cap)

## Implementation phases

Pure logic first, sockets on loopback, protocol wiring last.

1. **Negotiation parsing/formatting (TDD).** Pure functions for SEND / passive /
   RESUME / ACCEPT, uint32 IP encode/decode. No sockets.
2. **Transfer engine — receive (TDD).** Connect, stream to temp, ACK, atomic
   rename. End-to-end over loopback in tests.
3. **Transfer engine — send (TDD).** Listen, accept, stream. Loopback round-trip
   test (send → receive) verifying byte-identical files.
4. **Resume (TDD).** RESUME/ACCEPT offset; partial-file continuation.
5. **NAT module.** UPnP/NAT-PMP discovery + mapping; reachability detection;
   active/passive decision. Behind an interface so it can be mocked; real
   mapping is integration-tested manually.
6. **Manager + client.go routing + event emissions.** Wire DCC CTCP → manager;
   progress events.
7. **App methods (`app_files.go`) + `transfers` storage table.**
8. **Frontend Files view.**
9. **Docs** — `docs/files.md`: how it works, the download dir, NAT behavior, the
   known double-NAT limitation, the privacy/IP note.

## Testing

- Go: loopback end-to-end transfers (send↔receive, resume), negotiation unit
  tests, sanitization tests. `CGO_ENABLED=1 go test -tags fts5 ./internal/...`.
- NAT module via mocked interface; real UPnP/NAT-PMP verified manually.
- Frontend: vitest for the Files view state/actions.
- `task check` before done.

## Out of scope (explicitly)

- **Double-NAT with no port mapping** — no relay/hole-punching (classic DCC
  doesn't either). Clear failure state; future relay/Ergo tie-in.
- **DCC CHAT** — Files is file transfer only; DCC CHAT is a separate feature.
- **Secure DCC / TLS (SDCC)** — V2.
- **XDCC queue/management niceties** — receiving from an XDCC bot already works
  as a plain inbound transfer; no special bot UI in V1.

## Open questions to confirm during implementation

- Which Go UPnP / NAT-PMP library, and PCP support.
- IPv6 DCC encoding handling (clients vary).
- Default listen port and behavior when it's occupied.
- Where the download dir defaults, and the executable-extension warning policy.
