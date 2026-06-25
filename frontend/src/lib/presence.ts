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
export function dmPresenceState(nick: string, online: boolean | undefined): DmPresence {
  if (isServiceNick(nick)) return 'unknown';
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
