# Protocol references

Cascade implements the IRC client protocol against the community's canonical
specifications. These are the primary sources we check behavior against — use them when
adding a capability, parsing a new numeric or ISUPPORT token, or deciding how something
should render.

## Primary references

- **[IRC Definition Files — defs.ircdocs.horse](https://defs.ircdocs.horse/)**
  The de-facto registry of IRC protocol constants: numeric replies, channel/user modes,
  channel membership prefixes, channel types, [RPL_ISUPPORT tokens](https://defs.ircdocs.horse/defs/isupport),
  [client capabilities](https://defs.ircdocs.horse/defs/clientcaps),
  [message tags](https://defs.ircdocs.horse/defs/tags),
  [CTCP messages](https://defs.ircdocs.horse/defs/ctcp), extended bans, and the
  [formatting / colour codes](https://defs.ircdocs.horse/info/formatting). When in doubt
  about what a token or numeric means, start here.

- **[IRCv3 — ircv3.net](https://ircv3.net/)**
  The working group behind modern IRC extensions. The
  [specifications index](https://ircv3.net/specs/) covers every capability Cascade
  negotiates (SASL, server-time, message-tags, batch, labeled-response, chathistory,
  extended-monitor, the draft reply/react/typing/channel-context tags, and more). The
  [capability registry](https://ircv3.net/registry) lists ratified vs. draft status.

## Related

- **[Modern IRC client protocol — modern.ircdocs.horse](https://modern.ircdocs.horse/)**
  A readable, consolidated prose specification of the core client protocol (a practical
  successor to RFC 1459 / 2812), maintained alongside the definition files above.

## How Cascade uses these

- The IRCv3 capabilities Cascade negotiates and what it does with each are documented in
  [IRCv3 Support](ircv3-support.md), with a checkbox-tracked backlog in the
  [IRCv3 Roadmap](ircv3-roadmap.md).
- ISUPPORT-driven behavior (PREFIX, CHANMODES, CHANTYPES, CASEMAPPING, EXTBAN, BOT,
  MONITOR, WHOX, UTF8ONLY) is parsed from `RPL_ISUPPORT` (005) so Cascade adapts to each
  server rather than hardcoding assumptions.
