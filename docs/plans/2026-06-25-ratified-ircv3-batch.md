# Design: Remaining Ratified IRCv3 Extensions (batch PR)

**Date:** 2026-06-25
**Status:** Approved (implementing)
**Topic:** Close the last gaps in ratified IRCv3 compliance in a single PR.

## Why

`docs/public/developers/ircv3-support.md` asserts "every ratified IRCv3 capability is
now supported." Cross-checking `requestedCaps` (`internal/irc/client.go:31`) against the
live [IRCv3 extensions index](https://ircv3.net/irc/) shows that claim is **stale** — four
ratified, client-relevant extensions are not implemented:

| Extension | Type | Spec |
|-----------|------|------|
| `extended-monitor` | CAP | extends MONITOR to deliver AWAY/ACCOUNT/CHGHOST/SETNAME for monitored nicks |
| `UTF8ONLY` | ISUPPORT token | server only accepts UTF-8; client MUST send UTF-8 |
| `no-implicit-names` | CAP | server suppresses the implicit post-JOIN NAMES reply |
| `account-extban` | ISUPPORT (`EXTBAN`) | account-based ban masks (`$a` / `$a:account`) |

## Scope decision

All four, one PR (Matt's call). They split cleanly into backend-only compliance
(`UTF8ONLY`, `no-implicit-names`), an existing-infra reuse (`extended-monitor`), and a
ban-UI touch (`account-extban`).

## Per-extension design

### 1. `extended-monitor` (CAP)

The roster machinery already does the hard part: `applyUserMeta` stores meta
**unconditionally** (no channel-membership gate, `client.go:779`), and
`handleAway/handleAccount/handleChghost/handleSetname` already feed it. So the backend
change is one line: add `"extended-monitor"` to `requestedCaps`. Once negotiated, the
server sends those notifications for monitored-only nicks and the existing path stores them.

The gap is **display**: the Buddies pane (`monitor-list.tsx`) and DM dots (`presence.ts`)
show online/offline only — never away. With extended-monitor, a monitored buddy's away
state is now known even with no shared channel, so we surface it:

- Backend: `GetMonitorList` already returns presence; extend the entry (or have the
  frontend join against the existing `userMeta` store) so the Buddies pane can dim/annotate
  an away buddy.
- Frontend: Buddies pane shows an away affordance when the buddy is online-but-away.

Decision: reuse the **existing `usermeta-event` + `userMeta` store** rather than widening
the monitor event. The Buddies pane reads `getUserMeta(networkId, nick)` alongside presence.
No new backend event; minimal surface.

Gate the *display enrichment* behind nothing (meta is meta), but only request the cap when
offered (standard `requestedCaps` flow).

### 2. `UTF8ONLY` (ISUPPORT token)

Valueless token. The client already sends Go UTF-8 strings exclusively (no charset/latin1
path anywhere). So this is **record + comply-by-default**: parse the token in the `005`
handler, store `ServerCapabilities.UTF8Only`, expose via `GetServerCapabilities`. No
encoding change is required because we are already compliant; recording it documents
compliance and lets the UI note it if desired.

### 3. `no-implicit-names` (CAP)

**Sharp edge:** the nick list is built *solely* from the implicit post-JOIN `353`
(`client.go:1775`). WHOX fills only UserMeta, **not** membership prefixes. So if we
negotiate this cap and do nothing else, joined channels show a flat/empty roster.

Design: when `no-implicit-names` is enabled, send an explicit `NAMES <channel>` on our own
JOIN (`client.go:~1442`, the spot that currently relies on the implicit reply). The existing
`353`/`366` handlers then rebuild the roster identically. Net effect for Cascade — which
always wants the member list — is behavior-neutral but spec-compliant.

Implementation: replace the "we deliberately do NOT send NAMES" branch with a conditional —
if the cap is enabled, send `NAMES`; otherwise rely on the implicit reply as today.

Future optimization (out of scope, note in code): with the cap we *could* defer NAMES for
non-visible channels on a mass reconnect. Not doing that now.

### 4. `account-extban` (ISUPPORT `EXTBAN`)

`EXTBAN=<prefix>,<types>` (e.g. `EXTBAN=$,ajrxc`). Account extban is the `a` type with
prefix `$`, matching masks like `$a` (any logged-in user) or `$a:account`.

- Backend: parse `EXTBAN` in the `005` handler into `ServerCapabilities.ExtbanPrefix`
  (rune) + `ExtbanTypes` (set), expose via `GetServerCapabilities`.
- Frontend: in `channel-mode-editor.tsx`, render ban masks with a semantic label when they
  are extbans — `$a:alice` → "account: alice", `$a` → "any logged-in account". Only show the
  extban add hint when the server advertises the `a` type.

## Testing (TDD)

Go unit tests, mirroring existing per-cap files:
- `extended_monitor_test.go` — cap requested when offered; AWAY for a monitored-only nick
  flows through `applyUserMeta` and emits.
- `isupport_utf8only_test.go` — token parsed, `UTF8Only` set; absent → false.
- `no_implicit_names_test.go` — with cap enabled, self-JOIN sends `NAMES <chan>`; without,
  it does not; the `353`/`366` rebuild still works.
- `extban_test.go` — `EXTBAN=$,ajrxc` parsed to prefix `$` + types set; `extbanLabel($a:x)`.

Frontend (vitest): extban mask labeling helper; Buddies-pane away affordance.

Docs: update the capability matrix + "Not yet supported" section in
`ircv3-support.md`; bump `requestedCaps` snippet.

## Out of scope

`draft/*` (message-redaction, multiline, metadata-2, read-marker, typing/react/reply),
WebSocket/WebIRC (transport/server-side), deferred-NAMES reconnect optimization.
