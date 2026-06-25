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
| Bot mode `+B` user-mode half | High | ◐ | `BOT=` token in the `005` handler (`client.go:2449`); user-mode handling at `client.go:2024` |
| `+typing` (client-only tag) | High | ⛔ | New tag emit/parse; UI typing indicator |
| `+reply` / `+channel-context` | Medium | ⛔ | Pairs with stored `@msgid`; threaded-reply UI |
| `no-implicit-names` | Low | ⛔ | `requestedCaps` (`client.go:31`); suppress NAMES-driven roster seed, rely on WHOX |
| `UTF8ONLY` | Low | ⛔ | `005` handler; advertise/enforce UTF-8 |

### Detail

- [ ] **Bot mode `+B`** — Detection (the `bot` tag + RPL_WHOISBOT) is already done; this is the
      *persistent* half. Parse the `BOT=<letter>` ISUPPORT token (the `005` handler currently
      reads `PREFIX` / `CHANMODES` / `WHOX` / `MONITOR` only, `client.go:2449`), stop ignoring
      user modes for this case (`client.go:2024`), and add a path to set `+B` on self — relevant
      because Cascade's plugin system can host bots that should announce themselves. See the
      [Bot mode](ircv3-support.md#bot-mode) section. A prior design note exists:
      `docs/plans/2026-06-16-bot-mode-recognition.md`.
- [ ] **`+typing`** — Ratified client-only tag for real-time typing indicators. Emit `@+typing`
      on the local input path and parse inbound tags into a transient per-conversation state
      (same lifetime model as the session roster — never persisted). High UX visibility and the
      tag-parsing substrate (`message-tags`) already exists.
- [ ] **`+reply` / `+channel-context`** — Ratified client-only tags for threaded replies /
      quoting. Cascade already stores `@msgid` per message (used for history dedup), which is the
      anchor a reply points at — so the backend groundwork is partly there; the work is mostly
      emit + a reply-rendering UI.
- [ ] **`no-implicit-names`** — Ratified. Lets the client tell the server not to send the
      implicit NAMES burst on JOIN; Cascade already seeds the roster from WHOX, so this trims
      redundant traffic on large channels. Add to `requestedCaps` and gate the NAMES path.
- [ ] **`UTF8ONLY`** — Ratified ISUPPORT token. Cheap correctness win: parse it in the `005`
      handler and enforce/advertise UTF-8.

## Draft extensions (future modern-chat)

Out of scope for *ratified* compliance, but deployed on real networks (Ergo, Soju) and aligned
with Cascade's multi-platform story. Sequenced by value.

- [ ] **`draft/read-marker`** — Sync read position across devices. Highest-value draft for a
      multi-platform client; leans on the `chathistory` / `@msgid` machinery already in place.
- [ ] **`draft/message-redaction`** — Message delete/edit (`REDACT`). Needs message-mutation
      handling in storage + UI. The one draft already tracked (⛔) in the support matrix.
- [ ] **`draft/multiline`** — Send/receive true multi-line messages instead of split lines.
- [ ] **`+draft/react`** — Emoji reactions (client-only tag). UX polish.
- [ ] **`draft/extended-monitor`** — Builds on the existing MONITOR: away/account changes for
      monitored nicks without a separate WHOX. Low effort given MONITOR is done.
- [ ] **`draft/metadata-2`** — Avatars, status, display names. Larger effort.
- [ ] **`draft/channel-rename`** — Handle `RENAME` of a channel.
- [ ] **`draft/pre-away`** — Away-on-idle support.

## Suggested sequencing

1. **Bot mode `+B`** — closes the one ratified gap that's only *partial* today; design note exists.
2. **`+typing`** + **`draft/read-marker`** — the two features that most reinforce Cascade's
   multi-platform identity (live conversations; sessions stay in sync).
3. **`draft/message-redaction`** + **`draft/multiline`** — modern messaging table-stakes that
   Ergo/Soju already speak.
4. Mop up the cheap ratified items (`no-implicit-names`, `UTF8ONLY`) opportunistically.
5. UX polish (`+reply`, `+react`) and the larger `draft/metadata-2` later.

## When you implement a cap

Per the convention in [ircv3-support.md](ircv3-support.md#not-yet-supported): add the name to
`requestedCaps` (`client.go:31`), gate behavior on `enabledCaps` (or the relevant ISUPPORT
token), add unit tests (and an e2e screenshot where it adds visual signal), then move the entry
from this roadmap into the support doc's capability matrix.
