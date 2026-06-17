# Plan: Bot Mode Recognition

**Status:** Draft
**Date:** 2026-06-16
**Owner:** Matt

## Goal

Recognize and visually identify other users who are bots, per the
[IRCv3 Bot mode spec](https://ircv3.net/specs/extensions/bot-mode), across three
display surfaces:

1. **WHOIS panel** — show a "bot" indicator when the server returns numeric
   `335` (RPL_WHOISBOT).
2. **Chat rows** — a small "BOT" badge next to the sender's nick on messages
   from a bot.
3. **Nick list** — the same badge next to bot members in the channel user list.

This is purely about *recognizing* bots. Announcing Cascade itself as a bot
(`MODE +B`) is **out of scope** (see below).

## Key decisions (settled during brainstorming)

1. **One source of truth: a per-network in-memory set of bot nicks.** Bot status
   is a network-global property of a *nick*, not per-channel state. Modeling it
   as a single `IRCClient.knownBots` set (lowercased keys, mutex-guarded) — not a
   column on `channel_users` or `messages` — is the single input to all three
   surfaces. It also lets us retroactively badge a bot's *past* messages the
   moment the bot is identified.

2. **Lazy population (no proactive WHO).** The set is filled from two signals
   only:
   - the valueless `bot` message tag on incoming `PRIVMSG` / `NOTICE` / `ACTION`
     (per spec, any value is ignored), and
   - WHOIS numeric `335`.

   A silent bot stays unbadged until it speaks or is explicitly WHOIS'd. We do
   **not** issue WHO/WHOX on join. Accepted tradeoff for simplicity and zero
   extra traffic.

3. **In-memory, not persisted — deliberately.** Nick is not a reliable stable
   identity: a nick that is a bot today can be a human tomorrow, so persisting
   bot status keyed on nick would carry stale "BOT" badges across restarts. The
   only stable cross-session identity IRC offers is the **account name**
   (numeric `330` / `account-tag`), and bots frequently run unauthenticated, so
   we would often have nothing to key on. The set therefore lives only for the
   session and re-accrues after a restart as bots next speak / are WHOIS'd. This
   also means it survives NAMES/rejoin rebuilds (unlike a `channel_users`
   column, which is cleared on every rejoin).

   *Future enhancement (not now):* persist bot status keyed on **account name**,
   gated on the bot actually being authenticated.

4. **No new capability or ISUPPORT parsing needed.** The `bot` tag rides on the
   `message-tags` capability, already in `requestedCaps`
   (`internal/irc/client.go`). Because we never set `MODE +B`, we never need the
   `BOT=` ISUPPORT letter. We just read `e.GetTag("bot")` and numeric `335`.

## Data flow

```
bot-tagged PRIVMSG/NOTICE/ACTION ─┐
                                  ├─→ client.markBot(nick) ─→ EventBotDetected{network, nick}
WHOIS numeric 335 ────────────────┘                              │
                                                                 ▼
                                              frontend Zustand store: bots[networkId] = Set<nick>
                                                                 │
                        ┌────────────────────────┬───────────────┴───────────────┐
                        ▼                         ▼                               ▼
                  nick list badge          chat-row BOT badge              WHOIS panel
              (nick ∈ bots set)      (message.user ∈ bots set)      (whois.is_bot from 335)
```

## Backend (Go)

- **`IRCClient`** (`internal/irc/client.go`): add `knownBots map[string]bool`
  guarded by a mutex, plus a `markBot(nick string)` helper that inserts the
  lowercased nick and emits `EventBotDetected` **only on first discovery**
  (idempotent — repeated bot messages do not re-emit).
- **PRIVMSG / NOTICE / ACTION handlers**: when
  `present, _ := e.GetTag("bot"); present`, call `markBot(e.Nick())`. The tag is
  also present on `JOIN`/`MODE`/etc., but message-author surfaces are what we
  badge, so PRIVMSG/NOTICE/ACTION are sufficient; we may also mark from JOIN for
  earlier discovery (optional, low cost).
- **New `335` (RPL_WHOISBOT) handler**: set `WhoisInfo.IsBot = true` (new field
  on the struct in `internal/irc/events.go`, JSON `is_bot`) and call
  `markBot(nick)`.
- **New event** `EventBotDetected` with `{network, networkId, nick}`, routed to
  the frontend through the existing `app_events.go` forwarding.
- **New binding** `GetNetworkBots(networkID int64) []string` so a freshly opened
  or reloaded frontend can hydrate the current set.

## Frontend (React / Zustand)

- **Store**: `bots: Record<number, Set<string>>` keyed by `networkId`. Hydrate
  via `GetNetworkBots` on network select; append on `EventBotDetected`. All
  lookups use lowercased nicks.
- **Nick list** (`frontend/src/components/channel-info.tsx`): render a small
  "BOT" badge when the lowercased member nick is in the set.
- **Message rows**: same badge next to the author when the lowercased
  `message.user` is in the set — retroactively badges already-rendered history
  once the bot is known.
- **WHOIS** (`frontend/src/components/user-info.tsx`): a "Bot" indicator driven
  by `whois.is_bot`.

## Testing

- **Go**: a `bot`-tagged message marks the nick and emits `EventBotDetected`
  exactly once (second tagged message does not re-emit); numeric `335` sets
  `WhoisInfo.IsBot` and marks the nick; an untagged message does not mark.
  Fits alongside `internal/irc/handle_notice_test.go` / `client_test.go`.
- **Frontend**: vitest for the store reducer (hydrate + append, case-folding)
  and badge rendering given a populated set.

## Out of scope (YAGNI)

- Announcing Cascade as a bot (`MODE +B`) and parsing the `BOT=` ISUPPORT token.
- Proactive WHO/WHOX on join to discover silent bots.
- Persisting bot status across restarts (revisit only with account-name keying).
