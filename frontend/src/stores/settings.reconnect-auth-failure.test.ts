import { describe, it, expect, vi, beforeEach } from 'vitest';

const setSetting = vi.fn().mockResolvedValue(undefined);
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

describe('reconnectOnAuthFailure setting', () => {
  beforeEach(() => {
    setSetting.mockClear();
    changedHandler = undefined;
    for (const k of Object.keys(settingValues)) delete settingValues[k];
    useSettingsStore.setState({ reconnectOnAuthFailure: false });
  });

  it('defaults to false', () => {
    expect(useSettingsStore.getState().reconnectOnAuthFailure).toBe(false);
  });

  it('persists changes through SetSetting under the reconnect_on_auth_failure key', () => {
    useSettingsStore.getState().setReconnectOnAuthFailure(true);
    expect(useSettingsStore.getState().reconnectOnAuthFailure).toBe(true);
    expect(setSetting).toHaveBeenCalledWith('reconnect_on_auth_failure', 'true');
  });

  it('hydrates a persisted true value from the backend', async () => {
    settingValues['reconnect_on_auth_failure'] = 'true';
    await initSettings();
    expect(useSettingsStore.getState().reconnectOnAuthFailure).toBe(true);
  });

  it('stays false for an unset key (empty string)', async () => {
    // settingValues has no entry — GetSetting returns ''
    await initSettings();
    expect(useSettingsStore.getState().reconnectOnAuthFailure).toBe(false);
  });

  it('stays false for any value other than "true"', async () => {
    settingValues['reconnect_on_auth_failure'] = 'false';
    await initSettings();
    expect(useSettingsStore.getState().reconnectOnAuthFailure).toBe(false);
  });

  it('reconciles a cross-window setting:changed broadcast without writing back', async () => {
    await initSettings();
    expect(changedHandler).toBeDefined();
    changedHandler?.({ key: 'reconnect_on_auth_failure', value: 'true' });
    expect(useSettingsStore.getState().reconnectOnAuthFailure).toBe(true);
    // Reconciliation is in-memory only — it must not re-persist (no loop).
    expect(setSetting).not.toHaveBeenCalled();
  });

  it('reconciles back to false on setting:changed with non-true value', async () => {
    useSettingsStore.setState({ reconnectOnAuthFailure: true });
    await initSettings();
    changedHandler?.({ key: 'reconnect_on_auth_failure', value: 'false' });
    expect(useSettingsStore.getState().reconnectOnAuthFailure).toBe(false);
  });
});
