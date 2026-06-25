import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createTypingSender, shouldBroadcastTyping, REFRESH_MS, IDLE_MS } from './typing-sender';

describe('shouldBroadcastTyping', () => {
  it('broadcasts for ordinary message text', () => {
    expect(shouldBroadcastTyping('hello there')).toBe(true);
    expect(shouldBroadcastTyping('not /a command')).toBe(true);
  });
  it('does not broadcast for empty or whitespace-only text', () => {
    expect(shouldBroadcastTyping('')).toBe(false);
    expect(shouldBroadcastTyping('   ')).toBe(false);
  });
  it('does not broadcast while composing a slash command', () => {
    expect(shouldBroadcastTyping('/join #chan')).toBe(false);
    expect(shouldBroadcastTyping('/msg bob hi')).toBe(false);
    expect(shouldBroadcastTyping('  /quit')).toBe(false); // dispatch trims before classifying
  });
});

describe('typing sender state machine', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('sends active on the first keystroke', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.type();
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith('active');
  });

  it('throttles active to once per refresh window across rapid keystrokes', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.type();
    vi.advanceTimersByTime(500);
    s.type();
    vi.advanceTimersByTime(500);
    s.type();
    // Still within the first REFRESH_MS window: only the initial active.
    expect(send.mock.calls.filter((c) => c[0] === 'active')).toHaveLength(1);
    // Crossing a refresh boundary re-asserts active once.
    vi.advanceTimersByTime(REFRESH_MS);
    expect(send.mock.calls.filter((c) => c[0] === 'active')).toHaveLength(2);
  });

  it('transitions to paused after the idle window with no keystroke', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.type();
    vi.advanceTimersByTime(IDLE_MS);
    expect(send).toHaveBeenLastCalledWith('paused');
  });

  it('stops refreshing once paused', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.type();
    vi.advanceTimersByTime(IDLE_MS); // now paused
    send.mockClear();
    vi.advanceTimersByTime(IDLE_MS * 2);
    expect(send).not.toHaveBeenCalled();
  });

  it('returns to active when typing resumes after a pause', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.type();
    vi.advanceTimersByTime(IDLE_MS); // paused
    send.mockClear();
    s.type();
    expect(send).toHaveBeenCalledWith('active');
  });

  it('finish() sends done when composing', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.type();
    send.mockClear();
    s.finish();
    expect(send).toHaveBeenCalledExactlyOnceWith('done');
  });

  it('finish() sends nothing when idle', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.finish();
    expect(send).not.toHaveBeenCalled();
  });

  it('dispose() tears down without sending done and stops timers', () => {
    const send = vi.fn();
    const s = createTypingSender(send);
    s.type();
    send.mockClear();
    s.dispose();
    vi.advanceTimersByTime(IDLE_MS * 2);
    expect(send).not.toHaveBeenCalled();
  });
});
