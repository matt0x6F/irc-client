import { describe, it, expect, vi, beforeEach } from 'vitest';

const handlers: Record<string, (data: unknown) => void> = {};

vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn((name: string, cb: (data: unknown) => void) => {
    handlers[name] = cb;
    return () => delete handlers[name];
  }),
}));

const openOrJoinChannel = vi.fn();
const openQuery = vi.fn();
const setDeepLinkDisambiguation = vi.fn();

vi.mock('./network', () => ({
  useNetworkStore: { getState: () => ({ openOrJoinChannel, openQuery }) },
}));

vi.mock('./ui', () => ({
  useUIStore: { getState: () => ({ setDeepLinkDisambiguation }) },
}));

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenSettingsNetworks: () => { OpenSettingsNetworksCalled(); },
}));

const OpenSettingsNetworksCalled = vi.fn();

import { initDeepLinks } from './deeplink';

describe('initDeepLinks', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    initDeepLinks();
  });

  it('routes add-network by opening the settings window', () => {
    handlers['deeplink:add-network']({ host: 'x.net', port: 6697, tls: true, channel: '#c' });
    expect(OpenSettingsNetworksCalled).toHaveBeenCalled();
  });

  it('routes join to channel join and query open', async () => {
    handlers['deeplink:join']({
      networkId: 7,
      targets: [{ name: '#c', isNick: false, key: '' }, { name: 'bob', isNick: true, key: '' }],
    });
    // applyTargets is async; flush microtasks so the awaited calls resolve.
    await Promise.resolve();
    await Promise.resolve();
    expect(openOrJoinChannel).toHaveBeenCalledWith(7, '#c');
    expect(openQuery).toHaveBeenCalledWith(7, 'bob');
  });

  it('routes disambiguate into ui store', () => {
    const payload = { candidates: [{ networkId: 1, name: 'A' }], targets: [{ name: '#c', isNick: false, key: '' }] };
    handlers['deeplink:disambiguate'](payload);
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(payload);
  });
});
