// Channel-name detection, mirroring the backend's CHANTYPES-aware logic
// (internal/irc/spec.go: channelNameMatches). A target is a channel when its
// first character is one of the server's advertised channel-prefix characters.
//
// The server's CHANTYPES token is exposed via ServerCapabilitiesInfo and cached
// per network in the network store; callers that have a network in scope should
// pass it so modeless ('+') and safe ('!') channels are recognized. Callers
// without a network fall back to the conventional set, matching prior behavior.

// DEFAULT_CHANTYPES is the conventional channel-prefix set used before the
// server advertises CHANTYPES (and when it never does).
export const DEFAULT_CHANTYPES = '#&';

/** Reports whether `name` is an IRC channel (vs. a nick / PM target). */
export function isChannelName(name: string, chanTypes: string = DEFAULT_CHANTYPES): boolean {
  if (!name) return false;
  const types = chanTypes || DEFAULT_CHANTYPES;
  return types.includes(name[0]);
}
