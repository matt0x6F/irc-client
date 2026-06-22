import { describe, it, expect, vi, beforeEach } from 'vitest';

const setSetting = vi.fn((_key: string, _value: string) => Promise.resolve());
const settingValues: Record<string, string> = {};
vi.mock('../../wailsjs/go/main/App', () => ({
  GetSetting: vi.fn((key: string) => Promise.resolve(settingValues[key] ?? '')),
  SetSetting: (key: string, value: string) => setSetting(key, value),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn() }));

import { useSettingsStore } from './settings';

describe('notification settings', () => {
  beforeEach(() => {
    setSetting.mockClear();
    useSettingsStore.setState({
      notificationsEnabled: false,
      notifyPrivateMessages: true,
    });
  });

  it('persists the master toggle under notifications.enabled', () => {
    useSettingsStore.getState().setNotificationsEnabled(true);
    expect(useSettingsStore.getState().notificationsEnabled).toBe(true);
    expect(setSetting).toHaveBeenCalledWith('notifications.enabled', 'true');
  });

  it('persists per-event toggles', () => {
    useSettingsStore.getState().setNotifyPrivateMessages(false);
    expect(setSetting).toHaveBeenCalledWith('notifications.privateMessages', 'false');
  });
});
