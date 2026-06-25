import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  useTypingStore,
  typingNicksFor,
  formatTypingLabel,
  EXPIRE_MS,
} from './typing';

const apply = (ev: { networkId: number; target: string; nick: string; state: string }) =>
  useTypingStore.getState().applyTypingEvent(ev);

describe('typing store', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    useTypingStore.setState({ typing: {} });
  });
  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it('stores an active typer addressed to a channel under the channel key', () => {
    apply({ networkId: 1, target: '#go', nick: 'alice', state: 'active' });
    expect(typingNicksFor(useTypingStore.getState().typing, 1, '#go')).toEqual(['alice']);
  });

  it('normalizes a PM target to the pm:<nick> conversation key', () => {
    apply({ networkId: 1, target: 'carol', nick: 'carol', state: 'active' });
    expect(typingNicksFor(useTypingStore.getState().typing, 1, 'pm:carol')).toEqual(['carol']);
  });

  it('expires a typer after EXPIRE_MS with no refresh', () => {
    apply({ networkId: 1, target: '#go', nick: 'alice', state: 'active' });
    vi.advanceTimersByTime(EXPIRE_MS);
    expect(typingNicksFor(useTypingStore.getState().typing, 1, '#go')).toEqual([]);
  });

  it('done removes the typer immediately', () => {
    apply({ networkId: 1, target: '#go', nick: 'alice', state: 'active' });
    apply({ networkId: 1, target: '#go', nick: 'alice', state: 'done' });
    expect(typingNicksFor(useTypingStore.getState().typing, 1, '#go')).toEqual([]);
  });

  it('does not surface paused typers as active', () => {
    apply({ networkId: 1, target: '#go', nick: 'alice', state: 'paused' });
    expect(typingNicksFor(useTypingStore.getState().typing, 1, '#go')).toEqual([]);
  });

  it('clearNetwork drops every typer for that network', () => {
    apply({ networkId: 1, target: '#go', nick: 'alice', state: 'active' });
    apply({ networkId: 1, target: 'carol', nick: 'carol', state: 'active' });
    apply({ networkId: 2, target: '#rust', nick: 'bob', state: 'active' });
    useTypingStore.getState().clearNetwork(1);
    expect(typingNicksFor(useTypingStore.getState().typing, 1, '#go')).toEqual([]);
    expect(typingNicksFor(useTypingStore.getState().typing, 1, 'pm:carol')).toEqual([]);
    expect(typingNicksFor(useTypingStore.getState().typing, 2, '#rust')).toEqual(['bob']);
  });
});

describe('formatTypingLabel', () => {
  it('returns null for no typers', () => {
    expect(formatTypingLabel([])).toBeNull();
  });
  it('formats a single typer', () => {
    expect(formatTypingLabel(['alice'])).toBe('alice is typing…');
  });
  it('formats two typers', () => {
    expect(formatTypingLabel(['alice', 'bob'])).toBe('alice and bob are typing…');
  });
  it('aggregates three or more typers', () => {
    expect(formatTypingLabel(['alice', 'bob', 'carol'])).toBe('alice and 2 others are typing…');
  });
});
