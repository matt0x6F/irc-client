import { describe, it, expect, vi, beforeEach } from 'vitest';

const setSetting = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  GetSetting: vi.fn().mockResolvedValue(''),
  SetSetting: (...a: unknown[]) => setSetting(...a),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn() }));

import { usePreferencesStore } from './preferences';

describe('closeBufferOnLeave preference', () => {
  beforeEach(() => { setSetting.mockClear(); usePreferencesStore.setState({ closeBufferOnLeave: true }); });

  it('defaults to true', () => {
    expect(usePreferencesStore.getState().closeBufferOnLeave).toBe(true);
  });
  it('persists changes through SetSetting', () => {
    usePreferencesStore.getState().setCloseBufferOnLeave(false);
    expect(usePreferencesStore.getState().closeBufferOnLeave).toBe(false);
    expect(setSetting).toHaveBeenCalledWith('closeBufferOnLeave', 'false');
  });
});
