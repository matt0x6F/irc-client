import { describe, it, expect } from 'vitest';
import { isServiceNick, dmPresenceState } from './presence';

describe('isServiceNick', () => {
  it('recognizes well-known network services case-insensitively', () => {
    for (const n of ['NickServ', 'ChanServ', 'SaslServ', 'MemoServ', 'HostServ', 'OperServ', 'BotServ', 'Global', 'nickserv']) {
      expect(isServiceNick(n)).toBe(true);
    }
  });
  it('treats ordinary nicks as non-services', () => {
    for (const n of ['alittlefang', 'matt0x6f', 'alice', 'serveradmin']) {
      expect(isServiceNick(n)).toBe(false);
    }
  });
});

describe('dmPresenceState', () => {
  it('returns online when the nick is tracked and online', () => {
    expect(dmPresenceState('alice', true)).toBe('online');
  });
  it('returns offline when the nick is tracked and offline', () => {
    expect(dmPresenceState('alice', false)).toBe('offline');
  });
  it('returns unknown when presence is not tracked', () => {
    expect(dmPresenceState('alice', undefined)).toBe('unknown');
  });
  it('returns unknown for service nicks regardless of any presence value', () => {
    expect(dmPresenceState('NickServ', true)).toBe('unknown');
    expect(dmPresenceState('ChanServ', false)).toBe('unknown');
    expect(dmPresenceState('SaslServ', undefined)).toBe('unknown');
  });
});
