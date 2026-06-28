import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { main } from '../../wailsjs/go/models';

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

const OpenSettingsNetworksCalled = vi.fn();
const ConnectSavedNetworkMock = vi.fn((_id: number) => Promise.resolve());
let GetConnectionStatusMock = vi.fn((_id: number) => Promise.resolve(true));
let DrainPendingDeepLinkMock = vi.fn(() => Promise.resolve(null as main.PendingDeepLink | null));

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenSettingsNetworks: () => { OpenSettingsNetworksCalled(); },
  ConnectSavedNetwork: (id: number) => ConnectSavedNetworkMock(id),
  GetConnectionStatus: (id: number) => GetConnectionStatusMock(id),
  DrainPendingDeepLink: () => DrainPendingDeepLinkMock(),
}));

import { initDeepLinks, applyDeepLinkTargets } from './deeplink';

describe('initDeepLinks', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    GetConnectionStatusMock = vi.fn((_id: number) => Promise.resolve(true));
    DrainPendingDeepLinkMock = vi.fn(() => Promise.resolve(null as main.PendingDeepLink | null));
    initDeepLinks();
  });

  it('routes add-network by opening the settings window', () => {
    handlers['deeplink:add-network']({ host: 'x.net', port: 6697, tls: true, channel: '#c' });
    expect(OpenSettingsNetworksCalled).toHaveBeenCalled();
  });

  it('routes join to channel join and query open (already connected)', async () => {
    handlers['deeplink:join']({
      networkId: 7,
      targets: [{ name: '#c', isNick: false, key: '' }, { name: 'bob', isNick: true, key: '' }],
    });
    // applyDeepLinkTargets is async; flush microtasks
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    expect(ConnectSavedNetworkMock).not.toHaveBeenCalled();
    expect(openOrJoinChannel).toHaveBeenCalledWith(7, '#c', undefined);
    expect(openQuery).toHaveBeenCalledWith(7, 'bob');
  });

  it('routes disambiguate into ui store', () => {
    const payload = { candidates: [{ networkId: 1, name: 'A' }], targets: [{ name: '#c', isNick: false, key: '' }] };
    handlers['deeplink:disambiguate'](payload);
    expect(setDeepLinkDisambiguation).toHaveBeenCalledWith(payload);
  });

  it('drains cold-start buffered event on mount', async () => {
    DrainPendingDeepLinkMock = vi.fn(() => Promise.resolve({ event: 'deeplink:add-network', data: {} } as main.PendingDeepLink));
    initDeepLinks();
    await Promise.resolve();
    await Promise.resolve();
    expect(OpenSettingsNetworksCalled).toHaveBeenCalled();
  });
});

describe('applyDeepLinkTargets', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    GetConnectionStatusMock = vi.fn((_id: number) => Promise.resolve(true));
  });

  it('skips ConnectSavedNetwork when already connected', async () => {
    await applyDeepLinkTargets(5, [{ name: '#test', isNick: false, key: '' }]);
    expect(ConnectSavedNetworkMock).not.toHaveBeenCalled();
    expect(openOrJoinChannel).toHaveBeenCalledWith(5, '#test', undefined);
  });

  it('calls ConnectSavedNetwork when disconnected then joins', async () => {
    let callCount = 0;
    GetConnectionStatusMock = vi.fn((_id: number) => {
      callCount++;
      // First call: disconnected; subsequent calls: connected
      return Promise.resolve(callCount > 1);
    });
    await applyDeepLinkTargets(3, [{ name: '#foo', isNick: false, key: '' }]);
    expect(ConnectSavedNetworkMock).toHaveBeenCalledWith(3);
    expect(openOrJoinChannel).toHaveBeenCalledWith(3, '#foo', undefined);
  });

  it('threads channel key to openOrJoinChannel', async () => {
    await applyDeepLinkTargets(9, [{ name: '#secret', isNick: false, key: 'KEY123' }]);
    expect(openOrJoinChannel).toHaveBeenCalledWith(9, '#secret', 'KEY123');
  });

  it('opens query for nick targets', async () => {
    await applyDeepLinkTargets(9, [{ name: 'alice', isNick: true, key: '' }]);
    expect(openQuery).toHaveBeenCalledWith(9, 'alice');
    expect(openOrJoinChannel).not.toHaveBeenCalled();
  });
});
