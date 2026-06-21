import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const handlers: Record<string, (p: unknown) => void> = {};
const selectPane = vi.fn();
const clearActivity = vi.fn();
const focusMainWindow = vi.fn();

vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (p: unknown) => void) => {
    handlers[name] = cb;
    return () => delete handlers[name];
  },
}));
vi.mock('../../wailsjs/go/main/App', () => ({ FocusMainWindow: (...a: unknown[]) => focusMainWindow(...a) }));
vi.mock('../stores/network', () => ({
  useNetworkStore: { getState: () => ({ selectPane, clearActivity }) },
}));

import { registerNotificationRouting } from './useNotificationRouting';

let cleanup: (() => void) | undefined;

describe('notification routing', () => {
  beforeEach(() => {
    selectPane.mockClear();
    clearActivity.mockClear();
    focusMainWindow.mockClear();
  });

  afterEach(() => {
    cleanup?.();
    cleanup = undefined;
  });

  it('navigate selects the pane and focuses the window', () => {
    cleanup = registerNotificationRouting();
    handlers['notification:navigate']({ networkId: 7, target: 'pm:alice' });
    expect(selectPane).toHaveBeenCalledWith(7, 'pm:alice');
    expect(focusMainWindow).toHaveBeenCalled();
  });

  it('mark read clears the activity key', () => {
    cleanup = registerNotificationRouting();
    handlers['notification:markRead']({ networkId: 7, target: '#go' });
    expect(clearActivity).toHaveBeenCalledWith('7:#go');
  });

  it('navigate with empty target focuses window but does not select a pane', () => {
    cleanup = registerNotificationRouting();
    handlers['notification:navigate']({ networkId: 7, target: '' });
    expect(selectPane).not.toHaveBeenCalled();
    expect(focusMainWindow).toHaveBeenCalled();
  });

  it('markRead with empty target does not clear activity', () => {
    cleanup = registerNotificationRouting();
    handlers['notification:markRead']({ networkId: 7, target: '' });
    expect(clearActivity).not.toHaveBeenCalled();
  });
});
