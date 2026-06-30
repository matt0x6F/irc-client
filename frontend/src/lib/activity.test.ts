import { describe, it, expect } from 'vitest';
import { activityTargetForEvent, type ActivityNetwork } from './activity';

// Two Ergo networks that share the same (deprecated) address string — the exact
// situation that made address-based resolution leak activity across networks.
const NETWORKS: ActivityNetwork[] = [
  { id: 1, address: 'irc.ergo.test', nickname: 'matt' }, // "ErgoIRC"
  { id: 2, address: 'irc.libera.chat', nickname: 'matt' }, // "Libera.chat"
  { id: 3, address: 'irc.ergo.test', nickname: 'matt' }, // "Local Ergo" — same address as #1
];

describe('activityTargetForEvent', () => {
  it('keys channel activity by the event networkId, not the address', () => {
    const t = activityTargetForEvent(
      'message.received',
      { networkId: 2, network: 'irc.libera.chat', channel: '#programming' },
      NETWORKS,
    );
    expect(t).toEqual({ networkId: 2, paneKey: '#programming', activityKey: '2:#programming' });
  });

  // Regression: two networks share an address. A message on network 3's
  // #programming must badge 3:#programming — NOT 1:#programming (the first
  // network with that address, which address-based Array.find would have picked).
  it('does not leak activity to a different network sharing the same address', () => {
    const t = activityTargetForEvent(
      'message.received',
      { networkId: 3, network: 'irc.ergo.test', channel: '#programming' },
      NETWORKS,
    );
    expect(t?.activityKey).toBe('3:#programming');
    expect(t?.activityKey).not.toBe('1:#programming');
  });

  it('keys sent channel messages by networkId too', () => {
    const t = activityTargetForEvent(
      'message.sent',
      { networkId: 3, network: 'irc.ergo.test', target: '#programming' },
      NETWORKS,
    );
    expect(t?.activityKey).toBe('3:#programming');
  });

  it('routes a received PM to the sender pane on the right network', () => {
    const t = activityTargetForEvent(
      'message.received',
      { networkId: 2, channel: 'matt', user: 'alice' },
      NETWORKS,
    );
    expect(t).toEqual({ networkId: 2, paneKey: 'pm:alice', activityKey: '2:pm:alice' });
  });

  it('does not badge our own echoed PM (echo-message)', () => {
    const t = activityTargetForEvent(
      'message.received',
      { networkId: 2, channel: 'alice', user: 'matt' }, // user === our nick = echo
      NETWORKS,
    );
    expect(t).toBeNull();
  });

  it('ignores status and non-message events', () => {
    expect(
      activityTargetForEvent('message.received', { networkId: 1, channel: 'status' }, NETWORKS),
    ).toBeNull();
    expect(
      activityTargetForEvent('user.joined', { networkId: 1, channel: '#x' }, NETWORKS),
    ).toBeNull();
  });

  it('returns null when the network cannot be resolved', () => {
    expect(
      activityTargetForEvent('message.received', { networkId: 999, channel: '#x' }, NETWORKS),
    ).toBeNull();
  });

  it('falls back to address only when no networkId is present (legacy events)', () => {
    const t = activityTargetForEvent(
      'message.received',
      { network: 'irc.libera.chat', channel: '#chat' },
      NETWORKS,
    );
    expect(t?.activityKey).toBe('2:#chat');
  });
});
