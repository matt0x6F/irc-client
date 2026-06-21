// Pane routing: which buffer does a backend message-event belong to?
//
// Channel panes are keyed by their raw IRC name ("#ircv3"), the server log by
// "status", and private-message panes by "pm:<peer>" — where <peer> is the
// conversation partner, NOT the raw PRIVMSG target. For an inbound DM the raw
// target is *our* nick, so matching on it never lines up with the open "pm:<peer>"
// pane; that mismatch is what left DM panes refreshing only on the 2s poll.
//
// The backend already computes the peer (pmPeer) and now ships it as `pmTarget`,
// so routing is authoritative here rather than re-derived from the sender.

export interface RoutableEvent {
  // The raw target on received events: a channel, or (for DMs) the recipient nick
  // (which is our own nick). Sent events use `target` instead — see below.
  channel?: string | null;
  // The raw target on sent (message.sent) events: a channel or the recipient nick.
  target?: string | null;
  // The conversation peer for private messages (backend-computed). Empty/absent
  // for channel and server messages.
  pmTarget?: string | null;
  // Events are loose maps carrying other fields (user, message, network, ...).
  [key: string]: unknown;
}

// The buffer key an event belongs to: "#chan"/"&chan", "status", or "pm:<peer>".
// Returns null only when there is nothing routable (should not happen in practice).
export function eventPaneKey(e: RoutableEvent): string | null {
  if (e.pmTarget) return `pm:${e.pmTarget}`;

  // Received events carry `channel`; sent events carry `target`. Normalize the
  // same way the message handler does (`eventData.target || eventData.channel`).
  const raw = e.target ?? e.channel;
  if (raw === null || raw === undefined || raw === '') return 'status';
  if (raw.startsWith('#') || raw.startsWith('&')) return raw;

  // Non-channel target with no pmTarget: a sent DM, or a legacy received event
  // (pre-pmTarget). Treat the bare target as the PM peer so it routes to the DM pane.
  return `pm:${raw}`;
}

// Does an event belong to the currently-open pane? PM keys compare
// case-insensitively (IRC nicks fold case); channel and status keys compare
// exactly, matching how those panes are keyed elsewhere.
export function eventMatchesPane(e: RoutableEvent, selectedChannel: string | null): boolean {
  const key = eventPaneKey(e);
  if (key === null || selectedChannel === null) return false;
  if (key.startsWith('pm:') && selectedChannel.startsWith('pm:')) {
    return key.toLowerCase() === selectedChannel.toLowerCase();
  }
  return key === selectedChannel;
}
