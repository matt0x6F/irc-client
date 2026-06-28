# IRCv3 Roadmap

Outstanding IRCv3 work — the companion to [IRCv3 Support](ircv3-support.md), which documents
what Cascade negotiates **today**. This document tracks what's **next**: confirmed gaps,
their priority, and where in the code to start.

Legend: ✅ Done · ◐ Partial · ⛔ Not started

> **Every entry here was verified by grep**, not inferred from a checklist. (A capability can
> be present without appearing in `requestedCaps` — `sts` and `monitor`/`WHOX` arrive via
> `CAP LS` values and ISUPPORT tokens, so the absence of a name there is *not* evidence of a
> gap.) When you finish an item, move its details into [ircv3-support.md](ircv3-support.md)'s
> capability matrix and tick the box here.

## Ratified gaps (do these first)

These are part of the **ratified** IRCv3 spec, so they count toward baseline compliance.

| Capability | Priority | Status | Where to start |
|------------|:--------:|:------:|----------------|
| Bot mode `+B` | — | ✅ | Done — `BOT=` token → `BotModeChar`; WHO `B` flag → `markBot`; `identify_as_bot` setting → `MODE +<letter>` at connect; self-echo via `markSelfBotFromUserMode`. |
| `+typing` (client-only tag) | — | ✅ | Done (#78) — `handleTypingTag` parses inbound `+typing`; `SendTyping` emits it; transient per-conversation state drives the typing indicator in channels + PMs, with independent send/receive toggles. See the [support matrix](ircv3-support.md#capability-status-matrix). |
| `+reply` / `+channel-context` | — | ✅ | Done — inbound reply quote + jump; "in #channel" pill + sticky per-PM context. |
| `extended-monitor` | — | ✅ | Done (#77) — ratified, not draft; away/account/host/realname for monitored nicks. See the [support matrix](ircv3-support.md#capability-status-matrix). |
| `account-extban` | — | ✅ | Done (#77) — `EXTBAN` `$a`/`$a:account` masks in the mode editor. |
| `no-implicit-names` | — | ✅ | Done (#77) — explicit `NAMES` on self-join when the cap is enabled. |
| `UTF8ONLY` | — | ✅ | Done (#77) — recorded in the `005` handler; UTF-8 by construction. |

### Detail

- [x] **Bot mode `+B`** — Done. The `BOT=<letter>` ISUPPORT token is parsed into
      `ServerCapabilities.BotModeChar` (`applyISUPPORTToken`); the WHO/WHOX `B` flag is folded
      through the existing `markBot`; a durable per-network `identify_as_bot` setting sends
      `MODE <nick> +<letter>` after registration (gated on `BOT=` support, with a status-line
      warning if unsupported); and Cascade's own `+B` MODE echo calls `markSelfBotFromUserMode`
      → `markBot`. The ratified set is now fully complete. See
      [Bot mode](ircv3-support.md#bot-mode).
- [x] **`+typing`** — Done (#78). Ratified client-only tag for real-time typing indicators.
      `SendTyping` emits `@+typing` (active/paused/done) on the local input path; `handleTypingTag`
      parses inbound tags into transient per-conversation state (same lifetime model as the session
      roster — never persisted, self-echo dropped). Works in channels + PMs with independent
      send/receive toggles. See [Typing indicators](ircv3-support.md#typing-indicators-typing-client-tag).
- [x] **`+reply` / `+channel-context`** — Done. Inbound reply quotes rendered with quoted text
      and jump-to-original (cross-buffer); "in #channel" pill on PM messages; sticky per-PM
      channel context; "Message privately (re: #channel)" trigger from nick list. Accepts both
      `+draft/` prefixed and bare tag forms; emits `+draft/` forms. See
      [Reply quotes](ircv3-support.md#reply-quotes-draftreply-client-tag) and
      [Channel context](ircv3-support.md#channel-context-draftchannel-context-client-tag).
- [x] **`no-implicit-names`** — Done (#77). Note the correction to the original plan: WHOX seeds
      *attributes*, not membership prefixes, so we can't just "rely on WHOX" — when the cap is
      enabled we send an explicit `NAMES <channel>` on self-join (`namesOnSelfJoin`) and the
      `353`/`366` handlers rebuild the roster as before.
- [x] **`UTF8ONLY`** — Done (#77). Recorded in the `005` handler (`applyISUPPORTToken`); Cascade
      already emits UTF-8 exclusively, so no enforcement change was needed.

## Draft extensions (future modern-chat)

Out of scope for *ratified* compliance, but deployed on real networks (Ergo, Soju) and aligned
with Cascade's multi-platform story. Sequenced by value.

- [ ] **`draft/read-marker`** — Sync read position across devices. Highest-value draft for a
      multi-platform client; leans on the `chathistory` / `@msgid` machinery already in place.
- [ ] **`draft/message-redaction`** — Message delete/edit (`REDACT`). Needs message-mutation
      handling in storage + UI. The one draft already tracked (⛔) in the support matrix.
- [ ] **`draft/multiline`** — Send/receive true multi-line messages instead of split lines.
- [ ] **`+draft/react`** — Emoji reactions (client-only tag). UX polish.
- [x] **`extended-monitor`** — Done (#77). (Was listed here as `draft/extended-monitor`, but it is
      in fact **ratified** — hence its move to the gaps table above.) Builds on the existing
      MONITOR: away/account/host/realname for monitored nicks. Low effort because the live-roster
      `applyUserMeta` path is membership-agnostic.
- [ ] **`draft/metadata-2`** — Avatars, status, display names. Larger effort.
- [ ] **`draft/channel-rename`** — Handle `RENAME` of a channel.
- [ ] **`draft/pre-away`** — Away-on-idle support.

## Suggested sequencing

1. ~~**Bot mode `+B`**~~ — Done. The ratified set is now **fully complete**.
2. ~~**`+typing`**~~ (done in #78) + **`draft/read-marker`** — the two features that most reinforce
   Cascade's multi-platform identity (live conversations; sessions stay in sync).
3. **`draft/message-redaction`** + **`draft/multiline`** — modern messaging table-stakes that
   Ergo/Soju already speak.
4. ~~Mop up the cheap ratified items (`no-implicit-names`, `UTF8ONLY`).~~ Done in #77, along with
   `extended-monitor` and `account-extban`.
5. ~~UX polish (`+reply`, `+react`)~~ — `+reply` and `+channel-context` are done; `+draft/react` and the larger `draft/metadata-2` are still future.

## When you implement a cap

Per the convention in [ircv3-support.md](ircv3-support.md#not-yet-supported): add the name to
`requestedCaps` (`client.go:31`), gate behavior on `enabledCaps` (or the relevant ISUPPORT
token), add unit tests (and an e2e screenshot where it adds visual signal), then move the entry
from this roadmap into the support doc's capability matrix.
