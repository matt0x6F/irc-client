import { describe, it, expect } from 'vitest';
import { groupBySender, COLLAPSE_THRESHOLD } from '../invites-view';

const inv = (inviter: string, channel: string) => ({
  inviter, channel, trusted: false, receivedAt: '2026-06-28T12:00:00Z',
});

describe('groupBySender', () => {
  it('groups invites by inviter', () => {
    const g = groupBySender([inv('a', '#1'), inv('a', '#2'), inv('b', '#3')]);
    expect(g).toHaveLength(2);
    expect(g.find((x) => x.inviter === 'a')!.channels).toHaveLength(2);
  });

  it('collapses a sender at or above the threshold', () => {
    const many = Array.from({ length: COLLAPSE_THRESHOLD }, (_, i) => inv('flood', '#' + i));
    const g = groupBySender(many);
    expect(g[0].collapsed).toBe(true);
  });

  it('keeps a small sender expanded', () => {
    const g = groupBySender([inv('a', '#1'), inv('a', '#2')]);
    expect(g[0].collapsed).toBe(false);
  });
});
