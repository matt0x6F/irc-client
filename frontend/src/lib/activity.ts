// Activity (unread-badge) tracking: which buffer does an incoming message-event
// bump, and under what key?
//
// Unread counts are keyed by `${networkId}:${paneKey}` so that same-named
// channels on different networks (e.g. #programming on two servers) never
// collide. The networkId MUST come from the event's own `networkId` field — the
// unique primary key the backend ships with every message event — NOT from
// matching the event's `network` (a *deprecated*, non-unique address string;
// real addresses now live in the Servers table). Two networks can share an
// address, so resolving by address makes Array.find return whichever network
// sorts first, leaking one network's activity onto another's badge.

import { isChannelName } from './channel-name';

export interface ActivityNetwork {
  id: number;
  address: string;
  nickname?: string;
}

export interface ActivityEvent {
  // Unique network id (backend primary key). Authoritative.
  networkId?: number | string | null;
  // Deprecated address string; only a last-resort fallback when networkId is absent.
  network?: string | null;
  // Received events carry `channel`; sent events carry `target`.
  channel?: string | null;
  target?: string | null;
  // Sender (for echo detection on received PMs).
  user?: string | null;
}

export interface ActivityTarget {
  networkId: number;
  // The pane the activity belongs to: "#chan"/"&chan" or "pm:<peer>".
  paneKey: string;
  // The unread-counts Map key: `${networkId}:${paneKey}`.
  activityKey: string;
}

// Resolve the network this event belongs to by its unique id, falling back to
// the deprecated address only when no id is present (legacy/unforeseen events).
function resolveNetwork(
  e: ActivityEvent,
  networks: ActivityNetwork[],
): ActivityNetwork | undefined {
  if (e.networkId !== undefined && e.networkId !== null && e.networkId !== '') {
    const id = Number(e.networkId);
    const byId = networks.find((n) => n.id === id);
    if (byId) return byId;
  }
  if (e.network) return networks.find((n) => n.address === e.network);
  return undefined;
}

// Compute the activity target for a message-event, or null when the event should
// not badge anything (non-message event, status target, or our own echoed PM).
export function activityTargetForEvent(
  eventType: string,
  e: ActivityEvent,
  networks: ActivityNetwork[],
): ActivityTarget | null {
  if (eventType !== 'message.received' && eventType !== 'message.sent') return null;

  const network = resolveNetwork(e, networks);
  if (!network) return null;

  const target = e.target || e.channel;
  if (!target || target === 'status') return null;

  const isChannel = isChannelName(target);

  let paneKey: string | null = null;
  if (isChannel) {
    paneKey = target;
  } else {
    // With echo-message, our own sent PMs come back as a 'message.received'
    // event whose user is *us*. That isn't a new incoming message (the matching
    // 'message.sent' already tracked the conversation), so don't badge it — and
    // never key it to our own nick, which would create a phantom self-PM badge.
    // The conversation peer is the target, not the sender.
    const isEcho =
      eventType === 'message.received' &&
      !!network.nickname &&
      !!e.user &&
      e.user.toLowerCase() === network.nickname.toLowerCase();
    if (!isEcho) {
      const pmUser = eventType === 'message.received' ? e.user || null : target;
      if (pmUser) paneKey = `pm:${pmUser}`;
    }
  }

  if (!paneKey) return null;

  return {
    networkId: network.id,
    paneKey,
    activityKey: `${network.id}:${paneKey}`,
  };
}
