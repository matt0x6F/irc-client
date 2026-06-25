import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const handlers: Record<string, (p: unknown) => void> = {};
const applyTypingEvent = vi.fn();
const clearNetwork = vi.fn();
let typingReceive = true;

vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (p: unknown) => void) => {
    handlers[name] = cb;
    return () => delete handlers[name];
  },
}));
vi.mock('../stores/typing', () => ({
  useTypingStore: { getState: () => ({ applyTypingEvent, clearNetwork }) },
}));
vi.mock('../stores/settings', () => ({
  useSettingsStore: { getState: () => ({ typingReceive }) },
}));

import { registerTypingRouting } from './useTypingRouting';

let cleanup: (() => void) | undefined;

describe('typing routing', () => {
  beforeEach(() => {
    applyTypingEvent.mockClear();
    clearNetwork.mockClear();
    typingReceive = true;
  });
  afterEach(() => {
    cleanup?.();
    cleanup = undefined;
  });

  it('forwards a typing event to the store when receive is enabled', () => {
    cleanup = registerTypingRouting();
    handlers['typing-event']({ networkId: 7, target: '#go', nick: 'alice', state: 'active' });
    expect(applyTypingEvent).toHaveBeenCalledWith({
      networkId: 7,
      target: '#go',
      nick: 'alice',
      state: 'active',
    });
  });

  it('drops typing events when receive is disabled', () => {
    typingReceive = false;
    cleanup = registerTypingRouting();
    handlers['typing-event']({ networkId: 7, target: '#go', nick: 'alice', state: 'active' });
    expect(applyTypingEvent).not.toHaveBeenCalled();
  });

  it('clears a network on disconnect', () => {
    cleanup = registerTypingRouting();
    handlers['connection-status']({ networkId: 7, connected: false });
    expect(clearNetwork).toHaveBeenCalledWith(7);
  });

  it('does not clear on a connect (connected=true)', () => {
    cleanup = registerTypingRouting();
    handlers['connection-status']({ networkId: 7, connected: true });
    expect(clearNetwork).not.toHaveBeenCalled();
  });
});
