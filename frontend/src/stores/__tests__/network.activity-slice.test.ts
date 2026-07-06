import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetActivityItems: vi.fn().mockResolvedValue([
    { id: 1, network_id: 1, source_type: 'highlight', target: '#dev', actor: 'alice', preview: 'matt look', msgid: 'm1', keyword: '', seen: false, timestamp: '2026-07-06T12:00:00Z', trusted: false, expires_at: null },
    { id: 2, network_id: 1, source_type: 'pm', target: 'bob', actor: 'bob', preview: 'ping', msgid: '', keyword: '', seen: true, timestamp: '2026-07-06T11:00:00Z', trusted: false, expires_at: null },
  ]),
  MarkActivitySeen: vi.fn().mockResolvedValue(undefined),
  MarkAllActivitySeen: vi.fn().mockResolvedValue(undefined),
  DismissActivity: vi.fn().mockResolvedValue(undefined),
  ClearSeenActivity: vi.fn().mockResolvedValue(undefined),
  ClearAllActivity: vi.fn().mockResolvedValue(undefined),
}));

import { useNetworkStore } from '../network';
import { MarkActivitySeen, DismissActivity } from '../../../wailsjs/go/main/App';

describe('activity slice', () => {
  beforeEach(() => {
    useNetworkStore.setState({ activityItems: [] });
    vi.clearAllMocks();
  });

  it('loadActivityItems populates newest-first', async () => {
    await useNetworkStore.getState().loadActivityItems();
    const items = useNetworkStore.getState().activityItems;
    expect(items).toHaveLength(2);
    expect(items[0].id).toBe(1); // newer timestamp first
  });

  it('markActivitySeenMany calls the binding per id and flips local state', async () => {
    useNetworkStore.setState({ activityItems: [{ id: 1, seen: false } as any, { id: 2, seen: false } as any] });
    await useNetworkStore.getState().markActivitySeenMany([1]);
    expect(MarkActivitySeen).toHaveBeenCalledWith(1);
    expect(useNetworkStore.getState().activityItems.find((i) => i.id === 1)!.seen).toBe(true);
    expect(useNetworkStore.getState().activityItems.find((i) => i.id === 2)!.seen).toBe(false);
  });

  it('dismissActivity removes the item locally', async () => {
    useNetworkStore.setState({ activityItems: [{ id: 1 } as any, { id: 2 } as any] });
    await useNetworkStore.getState().dismissActivity(1);
    expect(DismissActivity).toHaveBeenCalledWith(1);
    expect(useNetworkStore.getState().activityItems.map((i) => i.id)).toEqual([2]);
  });
});
