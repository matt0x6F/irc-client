// Presence helpers shared by the DM list. Mirrors the backend's MONITOR-driven
// presence model (see internal/irc/client.go): a nick's dot is "online"/"offline"
// only when the server is actually tracking it via MONITOR; service pseudo-clients
// and untracked nicks render as "unknown" (a neutral dot), never a false green.

const SERVICE_NICKS = new Set([
  'nickserv', 'chanserv', 'saslserv', 'memoserv', 'hostserv', 'operserv', 'botserv', 'global',
]);

// isServiceNick reports whether nick is a well-known network service. Presence is
// meaningless for these, so the DM dot stays neutral and they are never monitored.
export function isServiceNick(nick: string): boolean {
  return SERVICE_NICKS.has(nick.toLowerCase());
}

export type DmPresence = 'online' | 'offline' | 'unknown';

// dmPresenceState maps a DM correspondent to its dot state. `online` is the live
// MONITOR presence for the nick (true/false), or undefined when it isn't tracked.
// `connected` is the network's settled connection state: while it is KNOWN
// disconnected (=== false) the server tracks no one, so any leftover presence is
// stale and the dot falls back to neutral — the same honest gate the Buddies
// panel applies. This is what actually enforces that fallback: the presence map
// is cleared on the setConnectionStatus disconnect path, but the connection can
// also settle to false via the status poll (loadConnectionStatus /
// refreshAllConnectionStatus), which leaves the map untouched — so without this
// gate a green dot lingers after a disconnected fresh launch. `undefined` (status
// not yet known) intentionally leaves the live dots alone.
export function dmPresenceState(
  nick: string,
  online: boolean | undefined,
  connected?: boolean,
): DmPresence {
  if (isServiceNick(nick)) return 'unknown';
  if (connected === false) return 'unknown';
  if (online === undefined) return 'unknown';
  return online ? 'online' : 'offline';
}

export type BuddyPresence = 'online' | 'away' | 'offline';

// buddyPresence maps a monitored buddy's live state to a three-way dot. With the
// ratified extended-monitor cap the server also pushes AWAY for monitored nicks
// (even ones we share no channel with), so an online-but-away buddy is shown
// distinctly from an active one. Away only applies while online.
export function buddyPresence(online: boolean, away: boolean): BuddyPresence {
  if (!online) return 'offline';
  return away ? 'away' : 'online';
}
