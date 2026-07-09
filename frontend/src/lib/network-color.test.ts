import { describe, it, expect } from 'vitest';
import { NETWORK_COLORS, networkColorKey, resolveNetworkColor } from './network-color';

describe('network-color', () => {
  it('has unique keys', () => {
    const keys = NETWORK_COLORS.map((c) => c.key);
    expect(new Set(keys).size).toBe(keys.length);
  });
  it('is deterministic for the same name', () => {
    expect(networkColorKey('Libera')).toBe(networkColorKey('Libera'));
  });
  it('returns a valid palette key from the fallback', () => {
    const key = networkColorKey('OFTC');
    expect(NETWORK_COLORS.some((c) => c.key === key)).toBe(true);
  });
  it('honors an explicit valid key', () => {
    const c = NETWORK_COLORS[2];
    expect(resolveNetworkColor(c.key, 'anything')).toEqual({ bg: c.bg, fg: c.fg });
  });
  it('falls back when key is missing or unknown', () => {
    const viaName = resolveNetworkColor(null, 'Libera');
    const fk = networkColorKey('Libera');
    const expected = NETWORK_COLORS.find((c) => c.key === fk)!;
    expect(viaName).toEqual({ bg: expected.bg, fg: expected.fg });
    expect(resolveNetworkColor('bogus', 'Libera')).toEqual({ bg: expected.bg, fg: expected.fg });
  });
});
