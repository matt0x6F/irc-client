import { describe, it, expect } from 'vitest';
import { casefold } from './casefold';

describe('casefold', () => {
  it('folds A–Z to a–z under every mapping', () => {
    for (const m of ['ascii', 'rfc1459', 'rfc1459-strict', '', 'utf8']) {
      expect(casefold(m, 'NiCk')).toBe('nick');
    }
  });

  it('rfc1459 folds []\\~ to {}|^ (default when mapping is empty)', () => {
    expect(casefold('rfc1459', 'Nick[a]')).toBe('nick{a}');
    expect(casefold('rfc1459', 'a\\b')).toBe('a|b');
    expect(casefold('rfc1459', 'up~')).toBe('up^');
    // Empty/absent mapping is rfc1459, the protocol default.
    expect(casefold('', 'Nick[a]')).toBe('nick{a}');
    // Same-identity check: the two spellings collapse to one key.
    expect(casefold('rfc1459', 'Nick[a]')).toBe(casefold('rfc1459', 'nick{a}'));
  });

  it('rfc1459-strict folds brackets but NOT ~', () => {
    expect(casefold('rfc1459-strict', 'Nick[a]')).toBe('nick{a}');
    expect(casefold('rfc1459-strict', 'up~')).toBe('up~');
  });

  it('ascii folds only A–Z, leaving brackets distinct', () => {
    expect(casefold('ascii', 'Nick[a]')).toBe('nick[a]');
    expect(casefold('ascii', 'Nick[a]')).not.toBe(casefold('ascii', 'nick{a}'));
  });

  it('unknown mappings (precis/utf8) fold conservatively as ascii', () => {
    expect(casefold('utf8', 'Nick[a]')).toBe('nick[a]');
    expect(casefold('precis', 'A\\B')).toBe('a\\b');
  });

  it('is idempotent and mapping-case-insensitive', () => {
    expect(casefold('RFC1459', 'Nick[a]')).toBe('nick{a}');
    expect(casefold('rfc1459', casefold('rfc1459', 'Nick[a]'))).toBe('nick{a}');
  });

  it('passes non-ASCII bytes through unchanged', () => {
    expect(casefold('rfc1459', 'håkan')).toBe('håkan');
  });
});
