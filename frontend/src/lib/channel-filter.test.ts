import { describe, it, expect } from 'vitest';
import { parseChannelFilter, matchesChannelFilter } from './channel-filter';

const ch = (channel: string, users: number, topic = '') => ({ channel, users, topic });

describe('parseChannelFilter', () => {
  it('parses a >N user-count minimum', () => {
    expect(parseChannelFilter('>50')).toEqual({ minUsers: 50, terms: [] });
  });

  it('parses a <N user-count maximum', () => {
    expect(parseChannelFilter('<100')).toEqual({ maxUsers: 100, terms: [] });
  });

  it('combines bounds and text terms, lowercasing and stripping glob stars', () => {
    expect(parseChannelFilter('>50 <200 *Linux*')).toEqual({
      minUsers: 50,
      maxUsers: 200,
      terms: ['linux'],
    });
  });

  it('treats an empty filter as no constraints', () => {
    expect(parseChannelFilter('   ')).toEqual({ terms: [] });
  });

  it('keeps a bare number as a text term (not a bound)', () => {
    expect(parseChannelFilter('42')).toEqual({ terms: ['42'] });
  });
});

describe('matchesChannelFilter', () => {
  it('applies >N as strictly greater', () => {
    const f = parseChannelFilter('>50');
    expect(matchesChannelFilter(ch('#a', 51), f)).toBe(true);
    expect(matchesChannelFilter(ch('#a', 50), f)).toBe(false);
    expect(matchesChannelFilter(ch('#a', 10), f)).toBe(false);
  });

  it('applies <N as strictly less', () => {
    const f = parseChannelFilter('<100');
    expect(matchesChannelFilter(ch('#a', 99), f)).toBe(true);
    expect(matchesChannelFilter(ch('#a', 100), f)).toBe(false);
  });

  it('matches text terms against name or topic', () => {
    const f = parseChannelFilter('linux');
    expect(matchesChannelFilter(ch('#linux', 5), f)).toBe(true);
    expect(matchesChannelFilter(ch('#chat', 5, 'all about linux'), f)).toBe(true);
    expect(matchesChannelFilter(ch('#windows', 5, 'gates'), f)).toBe(false);
  });

  it('requires all criteria to hold (conjunctive)', () => {
    const f = parseChannelFilter('>50 linux');
    expect(matchesChannelFilter(ch('#linux', 80), f)).toBe(true);
    expect(matchesChannelFilter(ch('#linux', 20), f)).toBe(false); // fails count
    expect(matchesChannelFilter(ch('#windows', 80), f)).toBe(false); // fails text
  });

  it('passes everything when the filter is empty', () => {
    const f = parseChannelFilter('');
    expect(matchesChannelFilter(ch('#anything', 0), f)).toBe(true);
  });
});
