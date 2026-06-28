# IRCv3 Roadmap

The companion to [IRCv3 Support](ircv3-support.md), which covers what Cascade
negotiates today. This page tracks what's left to build, in rough order of value.

Legend: ✅ Done · ◐ Partial · ⛔ Not started

A capability can be present without appearing in `requestedCaps`. `sts` and
`monitor`/`WHOX`, for example, arrive through `CAP LS` values and ISUPPORT
tokens, so a missing name there isn't proof of a gap. When you finish an item,
move its details into the capability matrix in
[ircv3-support.md](ircv3-support.md) and tick the box here.

## Ratified set: complete

Every ratified IRCv3 capability Cascade targets is now shipped, so the baseline
compliance work is done. The [support matrix](ircv3-support.md#capability-status-matrix)
holds the per-capability detail and code pointers; this is just the list.

- ✅ Bot mode `+B`
- ✅ `+typing` (#78)
- ✅ `+reply` / `+channel-context`
- ✅ `extended-monitor` (#77)
- ✅ `account-extban` (#77)
- ✅ `no-implicit-names` (#77)
- ✅ `UTF8ONLY` (#77)

## Draft extensions (future modern-chat)

These fall outside ratified compliance, but they're deployed on real networks
(Ergo, Soju) and fit Cascade's multi-platform direction. Listed in rough order
of value.

- [ ] **`draft/read-marker`** — Sync read position across devices. The
      highest-value draft for a multi-platform client; builds on the
      `chathistory` / `@msgid` machinery already in place.
- [ ] **`draft/message-redaction`** — Message delete and edit (`REDACT`). Needs
      message-mutation handling in storage and the UI. The one draft currently
      tracked (⛔) in the support matrix.
- [ ] **`draft/multiline`** — Send and receive true multi-line messages instead
      of split lines.
- [ ] **`+draft/react`** — Emoji reactions (client-only tag). UX polish.
- [ ] **`draft/metadata-2`** — Avatars, status, display names. Larger effort.
- [ ] **`draft/channel-rename`** — Handle `RENAME` of a channel.
- [ ] **`draft/pre-away`** — Away-on-idle support.

## What's next

`draft/read-marker` is the highest-value item: cross-device read state reinforces
Cascade's multi-platform story and builds on the `chathistory` / `@msgid`
machinery already in place. After that, `draft/message-redaction` and
`draft/multiline` bring Cascade in line with what Ergo and Soju already do.

## When you implement a cap

Follow the convention in
[ircv3-support.md](ircv3-support.md#not-yet-supported): add the name to
`requestedCaps` (`client.go:31`), gate behavior on `enabledCaps` (or the relevant
ISUPPORT token), add unit tests (and an e2e screenshot where it adds visual
signal), then move the entry from this roadmap into the support doc's capability
matrix.
