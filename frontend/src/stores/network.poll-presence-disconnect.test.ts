import { describe, it, expect, beforeEach, vi } from 'vitest';

const getConnectionStatus = vi.fn();
vi.mock('../../wailsjs/go/main/App', () => ({
  GetConnectionStatus: (...a: unknown[]) => getConnectionStatus(...a),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from './network';

// The connection-status POLL (loadConnectionStatus / refreshAllConnectionStatus) is
// a third writer of connectionStatus. On a disconnected fresh launch the state
// settles via the poll, not the event reducer — so the poll must ALSO drop stale
// MONITOR presence, or a green DM dot lingers after restart (reported 2026-07-06).
// The pollers route through setConnectionStatus (untimestamped = authoritative) so
// the reset + idempotency live in exactly one place.
describe('connection-status poll clears stale presence on disconnect', () => {
  beforeEach(() => {
    getConnectionStatus.mockReset();
    useNetworkStore.setState({
      networks: [{ id: 1 } as never],
      connectionStatus: {},
      connectionStatusAt: {},
      presence: { 1: { vdamewood: true } },
      monitor: { 1: [{ nick: 'vdamewood', online: true }] },
    });
  });

  it('loadConnectionStatus drops presence + buddy online when the poll sees disconnected', async () => {
    getConnectionStatus.mockResolvedValue(false);
    await useNetworkStore.getState().loadConnectionStatus(1);
    const s = useNetworkStore.getState();
    expect(s.connectionStatus[1]).toBe(false);
    expect(s.presence[1]).toEqual({});
    expect(s.monitor[1][0].online).toBe(false);
  });

  it('refreshAllConnectionStatus drops presence for a disconnected network', async () => {
    getConnectionStatus.mockResolvedValue(false);
    await useNetworkStore.getState().refreshAllConnectionStatus();
    const s = useNetworkStore.getState();
    expect(s.connectionStatus[1]).toBe(false);
    expect(s.presence[1]).toEqual({});
  });

  it('leaves presence intact when the poll still sees the network connected', async () => {
    getConnectionStatus.mockResolvedValue(true);
    await useNetworkStore.getState().loadConnectionStatus(1);
    const s = useNetworkStore.getState();
    expect(s.connectionStatus[1]).toBe(true);
    expect(s.presence[1]).toEqual({ vdamewood: true });
    expect(s.monitor[1][0].online).toBe(true);
  });

  it('is idempotent: an unchanged connected poll does not churn the connectionStatus ref', async () => {
    getConnectionStatus.mockResolvedValue(true);
    await useNetworkStore.getState().loadConnectionStatus(1);
    const first = useNetworkStore.getState().connectionStatus;
    await useNetworkStore.getState().loadConnectionStatus(1);
    const second = useNetworkStore.getState().connectionStatus;
    expect(second).toBe(first); // no new object -> no needless re-render on a steady poll
  });
});
