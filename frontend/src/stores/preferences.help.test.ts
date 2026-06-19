import { describe, it, expect, vi, beforeEach } from 'vitest';

const setSetting = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  GetSetting: vi.fn().mockResolvedValue(''),
  SetSetting: (...a: unknown[]) => setSetting(...a),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn() }));

import { usePreferencesStore } from './preferences';

describe('helpDisplayMode preference', () => {
  beforeEach(() => { setSetting.mockClear(); usePreferencesStore.setState({ helpDisplayMode: 'dialog' }); });

  it('defaults to dialog', () => {
    expect(usePreferencesStore.getState().helpDisplayMode).toBe('dialog');
  });
  it('persists changes through SetSetting', () => {
    usePreferencesStore.getState().setHelpDisplayMode('buffer');
    expect(usePreferencesStore.getState().helpDisplayMode).toBe('buffer');
    expect(setSetting).toHaveBeenCalledWith('help.display_mode', 'buffer');
  });
});
