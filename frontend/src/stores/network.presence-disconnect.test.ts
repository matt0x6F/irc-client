import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../../wailsjs/go/main/App', () => ({}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from './network';

// When a network drops we can no longer see anyone, so every MONITOR-derived
// presence claim must be dropped — otherwise a green dot lingers on an offline
// buddy while the socket is down. Presence reset is wired into setConnectionStatus
// so it inherits the same out-of-order watermark guard as the status itself.
describe('setConnectionStatus presence reset on disconnect', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      connectionStatus: {},
      connectionStatusAt: {},
      presence: { 1: { vdamewood: true, alittlefang: false } },
      monitor: { 1: [{ nick: 'vdamewood', online: true }] },
    });
  });

  it('clears DM presence and buddy online flags when the network disconnects', () => {
    useNetworkStore.getState().setConnectionStatus(1, false, 1000);
    const s = useNetworkStore.getState();
    expect(s.connectionStatus[1]).toBe(false);
    expect(s.presence[1]).toEqual({});
    expect(s.monitor[1][0].online).toBe(false);
  });

  it('leaves presence intact when the network is (or stays) connected', () => {
    useNetworkStore.getState().setConnectionStatus(1, true, 1000);
    const s = useNetworkStore.getState();
    expect(s.connectionStatus[1]).toBe(true);
    expect(s.presence[1]).toEqual({ vdamewood: true, alittlefang: false });
    expect(s.monitor[1][0].online).toBe(true);
  });

  it('does not clear presence for a stale (out-of-order) disconnect after a newer connect', () => {
    useNetworkStore.getState().setConnectionStatus(1, true, 2000); // newer: connected
    useNetworkStore.getState().setConnectionStatus(1, false, 1000); // older: dropped by watermark
    const s = useNetworkStore.getState();
    expect(s.connectionStatus[1]).toBe(true);
    expect(s.presence[1]).toEqual({ vdamewood: true, alittlefang: false });
  });

  it('is idempotent: a repeated disconnect keeps the same empty-presence reference', () => {
    useNetworkStore.getState().setConnectionStatus(1, false, 1000);
    const first = useNetworkStore.getState().presence[1];
    useNetworkStore.getState().setConnectionStatus(1, false, 1001);
    const second = useNetworkStore.getState().presence[1];
    expect(second).toBe(first); // no new object -> no needless re-render
  });
});
