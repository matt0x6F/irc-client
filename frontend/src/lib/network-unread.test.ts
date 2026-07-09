import { describe, it, expect } from 'vitest';
import { networkUnreadTotal } from './network-unread';

describe('networkUnreadTotal', () => {
  const m = new Map<string, number>([
    ['1:#a', 3],
    ['1:#b', 2],
    ['1:pm:bob', 1],
    ['2:#a', 5],
    ['10:#a', 4], // must not leak into network 1
  ]);
  it('sums only the given network', () => {
    expect(networkUnreadTotal(m, 1)).toBe(6);
    expect(networkUnreadTotal(m, 2)).toBe(5);
    expect(networkUnreadTotal(m, 10)).toBe(4);
  });
  it('is zero for an unknown network', () => {
    expect(networkUnreadTotal(m, 99)).toBe(0);
  });
});
