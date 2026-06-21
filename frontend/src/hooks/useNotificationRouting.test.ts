import { describe, it, expect, vi, beforeEach } from 'vitest';

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

describe('notification routing', () => {
  beforeEach(() => {
    selectPane.mockClear();
    clearActivity.mockClear();
    focusMainWindow.mockClear();
  });

  it('navigate selects the pane and focuses the window', () => {
    registerNotificationRouting();
    handlers['notification:navigate']({ networkId: 7, target: 'pm:alice' });
    expect(selectPane).toHaveBeenCalledWith(7, 'pm:alice');
    expect(focusMainWindow).toHaveBeenCalled();
  });

  it('mark read clears the activity key', () => {
    registerNotificationRouting();
    handlers['notification:markRead']({ networkId: 7, target: '#go' });
    expect(clearActivity).toHaveBeenCalledWith('7:#go');
  });
});
