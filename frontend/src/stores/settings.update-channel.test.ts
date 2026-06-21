import { describe, it, expect, vi, beforeEach } from 'vitest';

const setSetting = vi.fn().mockResolvedValue(undefined);
// GetSetting is keyed: hydration reads several settings via Promise.all, so the
// mock returns a per-key value. Tests override updateChannel's value as needed.
const settingValues: Record<string, string> = {};
vi.mock('../../wailsjs/go/main/App', () => ({
  GetSetting: vi.fn((key: string) => Promise.resolve(settingValues[key] ?? '')),
  SetSetting: (...a: unknown[]) => setSetting(...a),
}));
// Capture the setting:changed handler so we can simulate a cross-window broadcast.
let changedHandler: ((payload: { key: string; value: string }) => void) | undefined;
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn((event: string, cb: (payload: { key: string; value: string }) => void) => {
    if (event === 'setting:changed') changedHandler = cb;
  }),
}));

import { useSettingsStore, initSettings } from './settings';

describe('updateChannel setting', () => {
  beforeEach(() => {
    setSetting.mockClear();
    changedHandler = undefined;
    for (const k of Object.keys(settingValues)) delete settingValues[k];
    useSettingsStore.setState({ updateChannel: 'stable' });
  });

  it('defaults to stable', () => {
    expect(useSettingsStore.getState().updateChannel).toBe('stable');
  });

  it('persists changes through SetSetting under the updateChannel key', () => {
    useSettingsStore.getState().setUpdateChannel('prerelease');
    expect(useSettingsStore.getState().updateChannel).toBe('prerelease');
    expect(setSetting).toHaveBeenCalledWith('updateChannel', 'prerelease');
  });

  it('hydrates a persisted prerelease value from the backend', async () => {
    settingValues['updateChannel'] = 'prerelease';
    await initSettings();
    expect(useSettingsStore.getState().updateChannel).toBe('prerelease');
  });

  it('keeps the default for an unset or unknown persisted value', async () => {
    settingValues['updateChannel'] = 'garbage';
    await initSettings();
    expect(useSettingsStore.getState().updateChannel).toBe('stable');
  });

  it('reconciles a cross-window setting:changed broadcast without writing back', async () => {
    await initSettings();
    expect(changedHandler).toBeDefined();
    changedHandler?.({ key: 'updateChannel', value: 'prerelease' });
    expect(useSettingsStore.getState().updateChannel).toBe('prerelease');
    // Reconciliation is in-memory only — it must not re-persist (no loop).
    expect(setSetting).not.toHaveBeenCalled();
  });
});
