# Invites: Sending & Receiving — Design

**Date:** 2026-06-28
**Status:** Approved (pending spec review)

## Problem

The IRC `INVITE` feature is half-wired and has UX and safety gaps:

- **Sending via slash** requires both arguments (`commands_registry.go`, `INVITE` spec `MinArgs: 2`), so `/invite <user>` errors instead of defaulting to the current channel.
- **Sending via UI** exists only in the channel member-list right-click menu (`channel-info.tsx`), and is gated behind `canInvite()` which requires op/halfop/voice. A regular member sees no button — but IRC lets any member invite to a non-invite-only channel, so the gate is wrong. There is no entry point from a PM/buddy or from a channel node.
- **Receiving** drops the invite silently into the network status buffer as a `MessageType:"invite"` line (`internal/irc/client.go` `handleInvite`), rendered with a clickable channel that joins (`message-view.tsx` `renderInviteText`). It's easy to miss, has no actions beyond join, and — if made louder — would become a harassment/nuisance vector with no sender filtering.

## Goals

- `/invite <user>` defaults to the current channel; `/invite <user> #channel` stays explicit.
- Multiple, discoverable UI entry points for sending invites.
- A receive experience that gets your attention for invites that matter while being resistant to nuisance/harassment flooding.
- No new global subsystems (no global ignore list, no new DB tables).

## Non-Goals (YAGNI)

- **No global `/ignore` feature.** `/ignore` stays unimplemented (`commands_registry.go` currently returns "not yet implemented"). The invite "block" introduced here is session-only and invite-scoped.
- **No persistent invite store / schema change.** Received invites are session-only and in-memory.
- **No accept/decline negotiation protocol.** "Accepting" an invite is simply joining the channel.

---

## Receiving invites

### Trust model (drives attention)

An inbound `INVITE` is classified at receipt:

- **Target is me, inviter is on the MONITOR/buddy list, not invite-blocked → trusted → notify.**
  - In-app badge / unread indicator on the Invites node and the network node.
  - Native OS notification **only if** the existing global notification opt-in is enabled (reuse `app_notifications.go`); invites do not get a special always-on notification channel.
  - No in-app toast.
- **Target is me, inviter not a buddy, not blocked → untrusted → quiet.**
  - Collected in the Invites node, contributes to the unread count, but fires no OS notification.
- **Inviter is invite-blocked → dropped silently** (not stored, no badge).
- **Target is not me** (the `invite-notify` "ops FYI" form, where someone invites a third party to a channel you operate) → remains a **plain status-buffer informational line** as today; never enters the Invites node and never badges.

Trust is **buddy-list-only by deliberate choice.** Sharing a channel or having an open PM with the inviter does *not* make them trusted — this keeps the notification surface tight against harassment. The trust flag is computed server-side via `GetMonitorList` / the `monitored_nicks` data.

### Attention level is a preference

The attention behavior is a user setting persisted via the existing settings table (`App.Get/SetSetting`, per the durable-prefs pattern — `localStorage` is not durable in WKWebView). Levels:

- **`trusted` (default):** trusted senders notify; everyone else is quiet. Described above.
- **`quiet`:** nothing ever notifies; all invites collect in the Invites node (pull-only).
- **`all`:** every invite notifies (still subject to the OS-notification opt-in, dedup, grouping, caps, and the block set).

Only the default (`trusted`) is required for the first cut; the other levels are a thin switch over the same classification path. The setting changes *whether the OS notification / badge-as-notification fires*; it never changes storage, grouping, or the block behavior.

The **invite TTL** is also a setting (default 24h), persisted the same way.

### Invites node (per network)

- A dedicated **"Invites"** item under each network in the server tree (sibling of the status buffer), shown with an unread count when there are unseen invites.
- Clicking opens a list of pending invites. Rows are **grouped by sender**.
- Per-row / per-channel action: **Join** (joins the channel; removes the entry).
- Per-sender (group) actions: **Ignore sender** (adds the nick to the session invite-block set and removes all their pending entries) and **Dismiss all**.

### Flood resistance — layered, non-destructive

Grouping and collapse are **purely presentational. Nothing is ever auto-dropped or auto-blocked.** The only ways an invite leaves the list are (a) the user joins it, (b) the user dismisses it, (c) the user clicks "Ignore sender," or (d) the high safety caps below.

Defenses, by axis:

1. **Repetition (same sender, same channel):** dedup by `(sender, channel)` — re-inviting to `#a` 50 times yields one entry (timestamp refreshed). *Backend, source of truth.*
2. **Breadth (one sender, many channels):** **group by sender.** Each sender is one row. *Frontend, presentation.*
3. **Volume-based collapse:** a sender's group is shown **expanded as individual channel rows when small (< 5 channels)** and **auto-collapsed into a single summary row when large (≥ 5 channels)**. The collapsed summary is **count-only — "stranger123 invited you to 8 channels," with no channel names rendered** — so attacker-controlled channel-name strings (a harassment vector in themselves) never reach the user unless they deliberately expand. The collapsed row carries the group-level actions (**Ignore sender**, **Dismiss all**), so a flood is one click to clear whether it holds 5 entries or the cap. Collapse triggers on **volume alone, never on trust** — a friend enthusiastically inviting a newcomer to several channels behaves exactly like anyone else: a short list shows normally; only a genuine flood collapses, and even then nothing is hidden, just tidied. *Frontend, presentation.*
4. **Safety caps (backstops):** keep at most **10 channels per sender** (surplus dropped with a "+N more" affordance) and an overall per-network cap on senders. Ten sits comfortably above realistic legitimate multi-invites while halving worst-case attacker-controlled-string exposure on expand. *Backend.*
5. **Staleness (TTL):** each invite carries a received-at timestamp and **auto-expires after a TTL (configurable, default 24h)**. Expired entries are excluded from the list, the counts, and the badge, then swept. This anchors the thresholds to **wall-clock time rather than session lifetime** — "5 channels" always means *5 within the TTL window*, consistent whether the app has been running for minutes or weeks. *Backend.*

> **New-user/friend rationale:** on day one a newcomer has not added their friend as a buddy, so the friend is "untrusted" → quiet (no OS notification) but fully visible in the Invites node. Because collapse keys on volume, not trust, and never suppresses, a friend's handful of invites renders as a normal, individually-joinable short list.

### State & storage

- **Session-only, in-memory, backend-owned.** `handleInvite` stops writing actionable (target-is-me) invites to the status buffer; instead it records them (with a received-at timestamp) in a per-network, mutex-guarded in-memory store on `App`, applies dedup/caps/block/TTL, tags the `trusted` flag, and emits an event. The frontend renders the node from a bound getter plus live events (same pattern as PM conversations and monitor presence). Restart clears all invites and the block set. No schema/sqlc change.
- **TTL expiry** is lazy-on-read (the getter and count/badge computations filter out entries older than the TTL) plus a **low-frequency sweep** (every few minutes) that drops expired entries from memory and emits an update so the badge clears on its own without user interaction. No per-entry timers.
- The **invite-block set** is in-memory, per network, session-only, invite-scoped.

### Removed behavior

- The status-buffer rendering of actionable invites is removed: `renderInviteText` and the `isInvite` rendering path in `message-view.tsx`, plus the associated test in `message-view.test.tsx`. (The third-party `invite-notify` informational status line remains.)

---

## Sending invites

### Slash: current-channel default

Mirror the existing `/me` expansion in `frontend/src/stores/network.ts` (the send-message path that prepends the active channel for `/me`):

- `/invite <user>` with exactly one argument, while the active pane is a real channel → expand to `/invite <user> <selectedChannel>` before `SendCommand`.
- Two or more arguments → pass through unchanged.
- One argument while in a status/PM pane → friendly inline error: "open a channel, or use `/invite <user> #channel`".

Backend `cmdInvite` is unchanged (`INVITE %s %s`); defaulting lives in the frontend where pane context exists.

### UI entry points (three)

1. **PM / buddy menu** (`server-tree.tsx`, `pm` context menu): add an **"Invite to ▸"** submenu listing the user's joined channels on that network; selecting one sends `/invite <user> <channel>`.
2. **Channel menu** (`server-tree.tsx`, `channel` context menu): add **"Invite user…"** which prompts for a nickname and sends `/invite <nick> <thatChannel>`.
3. **Member-list user menu** (`channel-info.tsx`): replace the single gated "Invite" item with an **"Invite to ▸"** submenu of the user's *other* joined channels (excludes the current one).

### Gate fix

Remove the `canInvite()` op/voice restriction from the member-list action. For "invite to another channel," the user's status in the *current* channel is irrelevant; only the server knows the target channel's `+i`/operator rules. Show the action and let the server enforce, surfacing the result (below).

### Send feedback (new)

Surface the server's response to an `INVITE` rather than fire-and-forget:

- `341 RPL_INVITING` → confirmation line: "Invited X to #chan".
- Map failures to friendly lines: `401` (no such nick), `442` (you're not on that channel), `443` (X is already on #chan), `482` (you're not a channel operator).

Extend the numeric handling that already exists in `internal/irc/recovery.go` / `client.go`. Feedback is shown as a status/channel line.

---

## Architecture split

- **Backend (Go) = source of truth.** Receives `INVITE`; classifies trust (`GetMonitorList`); applies dedup, caps, and the session block set; stores the per-network in-memory list; emits events; exposes bound getters/actions (list invites, dismiss, ignore-sender). Maps `INVITE` send-result numerics to feedback. Fires OS notifications for trusted invites via the existing opt-in path.
- **Frontend (React/Zustand) = presentation + command expansion.** Renders the Invites node (grouping by sender, volume-based collapse, actions). Expands `/invite <user>` to include the active channel. Adds the three send menus.

## Testing

- **Go unit tests:** trust classification (buddy / non-buddy / blocked); dedup by (sender, channel); per-sender cap (10) and per-network cap; TTL expiry (entries past the TTL excluded from list/counts; sweep drops them and emits) using an injectable clock; the block set; third-party `invite-notify` stays a status line; send-result numeric → feedback mapping.
- **Frontend tests:** `/invite` current-channel expansion (mirror the existing `/me` test); Invites-node grouping, volume-collapse threshold, and the Join / Dismiss / Ignore-sender actions.
- **e2e:** one screenshot of the Invites node (per the project's IRCv3 "visual signal only" bar).

## Open decisions captured (all resolved)

- Receive attention model = configurable preference; **default = trusted (buddy) notifies, rest quiet.**
- Trust signals = **buddy/MONITOR only**; ignored/blocked always suppressed.
- Notify = in-app badge + native OS notification (respecting existing opt-in); no toast.
- Invites UI = **dedicated per-network Invites node.**
- Persistence = **session-only, in-memory.**
- Block = **session-only, invite-scoped** (not a global `/ignore`).
- Collapse = **by sender, volume-triggered (≥ 5), non-destructive, count-only summary.**
- Per-sender cap = **10**; group actions (Ignore sender / Dismiss all) act from the collapsed row.
- Staleness = **per-invite TTL, configurable, default 24h**; thresholds/counts operate over pending-within-TTL, anchoring them to wall-clock time, not session length.
- Send UI = **all three** entry points + slash default + gate fix + send feedback.
