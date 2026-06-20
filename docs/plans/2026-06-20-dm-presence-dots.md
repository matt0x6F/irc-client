# DM-list presence dots via auto-MONITOR

**Branch:** `matt/dm-presence-dots`

## Problem

In the DIRECT MESSAGES sidebar, every entry shows a hardcoded green
`--presence-online` dot ([server-tree.tsx:457](../../frontend/src/components/server-tree.tsx)).
It tracks no presence — so a DM contact who is genuinely offline (and shows
offline in the Buddies/MONITOR pane) still reads as a green "online" dot in the
DM list. The dot borrows the visual language of presence without the data.

## Decision (from product owner)

- **Auto-monitor open PMs:** every open PM correspondent is MONITORed so the dot
  reflects real presence — added on open, removed on close.
- **Service nicks** (NickServ/ChanServ/SaslServ/…) get a neutral dot and are
  never monitored.

## Core architectural rule

A nick stays MONITORed on the server iff:

```
isDurableBuddy(nick)                       // explicit Buddies-pane entry, persisted
OR (hasOpenPM(nick) && !isSelf && !isServiceNick)   // transient, session-derived
```

The **curated Buddies list stays curated**: auto-monitored PM correspondents are
a *separate, transient source* and do **not** appear in the Buddies pane or in
`monitored_nicks` storage. Opening a DM must never add someone to your buddy list
forever. Both the buddy path and the PM path feed one idempotent `reconcile`
that computes the desired armed state and sends `MONITOR +`/`-` to match.

## Backend (Go)

1. **`internal/irc/client.go`**
   - Track the armed set: `monitorArmed map[string]bool` (guarded by `monitorMu`).
     `MonitorAdd`/`MonitorRemove`/`sendInitialMonitor` maintain it; reconcile uses
     it to avoid redundant sends.
   - Extract the `MONITOR=<limit>` value in the 005 handler into `monitorLimit int`.
   - Handle numeric **734 (MONLISTFULL)**: log; leave extra nicks unmonitored
     (graceful — their dots fall back to "unknown"/neutral).
   - `reconcileMonitor(nick, desired bool)`: arm/disarm against `monitorArmed`,
     respecting `monitorLimit` for auto (PM) adds (durable buddies take priority).
   - New bound-method support: `MonitorPresenceSnapshot()` already exists as
     `MonitorPresence()` — expose via app method below.
2. **`app.go`**
   - `isServiceNick(nick)` helper: hardcoded list (NickServ, ChanServ, SaslServ,
     MemoServ, HostServ, OperServ, BotServ, Global, …) + existing `isBot`.
   - `SetPrivateMessageOpen`: after toggling `is_open`, reconcile the nick
     (desired = open && !self && !service && supported).
   - `AddMonitor`/`RemoveMonitor`: route through reconcile so removing a buddy
     who still has an open PM keeps them monitored.
   - `sendInitialMonitor` (on connect): arm union of durable buddies + open PM
     correspondents (filtered).
   - `GetMonitorPresence(networkID) (map[string]bool, error)`: lowercased nick →
     online, for the frontend to seed DM-dot presence (covers events fired before
     the listener mounts).

## Frontend (TS/React)

3. **`stores/network.ts`**
   - Add `presence: Record<number, Record<string, boolean>>` (lowercased nick →
     online) + `setPresence(networkId, nick, online)` and a hydrate that calls
     `GetMonitorPresence`. This is the general presence map for DM dots
     (the buddy pane keeps its own `monitor[].online`, fed by the same event).
4. **`App.tsx`** — the existing `monitor-event` listener also calls `setPresence`.
5. **`server-tree.tsx`** — DM dot reads `presence[networkId]?.[user.toLowerCase()]`
   and `isServiceNick(user)`:
   - `true` → `--presence-online` (green, solid)
   - `false` → `--presence-offline` (gray, solid)
   - service nick OR `undefined` → neutral hollow dot (no fill, faint border) +
     `title` "presence unknown".
   - `title` reflects Online/Offline/Unknown for hover clarity.

## Tests

- Go: reconcile/union logic — buddy-only, PM-only, both, close-with-buddy,
  self/service exclusion, limit guard (table-driven, no live socket).
- Go: 005 `MONITOR=<limit>` parse; 734 handling is a no-op-but-logged path.
- Frontend (vitest): dot-state mapping (online/offline/unknown/service) pure
  helper; service-nick classifier.

## Docs

- Update `docs/ircv3-support.md` MONITOR section to describe auto-monitoring of
  open PMs and the curated-vs-transient distinction.

## Out of scope

- Persisting auto-monitor membership (it's derivable from open PMs each session).
- WHO/ISON fallback when the server lacks MONITOR (dot stays neutral — honest).
