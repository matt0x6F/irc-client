import { describe, it, expect } from 'vitest';
import { isServiceNick, dmPresenceState, buddyPresence } from './presence';

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

  // Presence is only meaningful while the server is actually tracking nicks. When
  // the network is KNOWN disconnected (connected === false) a leftover presence
  // entry is stale, so the dot must fall back to neutral — otherwise a green dot
  // lingers after a fresh launch whose connection settled via the status poll
  // (which bypasses the setConnectionStatus presence-clear). Mirrors the Buddies
  // panel's connection gate. Fresh launch / disconnected-restart regression.
  it('returns unknown when the network is disconnected, even if presence says online', () => {
    expect(dmPresenceState('vdamewood', true, false)).toBe('unknown');
    expect(dmPresenceState('vdamewood', false, false)).toBe('unknown');
  });
  it('uses the live presence value when connected', () => {
    expect(dmPresenceState('alice', true, true)).toBe('online');
    expect(dmPresenceState('alice', false, true)).toBe('offline');
  });
  it('leaves the live dots alone while connection state is still unknown (undefined)', () => {
    expect(dmPresenceState('alice', true, undefined)).toBe('online');
    expect(dmPresenceState('alice', false, undefined)).toBe('offline');
  });
});

describe('buddyPresence', () => {
  it('is offline when the buddy is not online, regardless of away', () => {
    expect(buddyPresence(false, false)).toBe('offline');
    expect(buddyPresence(false, true)).toBe('offline');
  });
  it('is online when the buddy is online and not away', () => {
    expect(buddyPresence(true, false)).toBe('online');
  });
  it('is away when the buddy is online but away (extended-monitor)', () => {
    expect(buddyPresence(true, true)).toBe('away');
  });
});
