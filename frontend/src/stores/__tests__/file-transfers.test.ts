import { beforeEach, describe, expect, it, vi } from 'vitest';

const { listHistory } = vi.hoisted(() => ({
  listHistory: vi.fn().mockResolvedValue({ transfers: [], nextCursor: '' }),
}));
vi.mock('../../../wailsjs/go/main/App', () => ({
  GetActiveFileTransfers: vi.fn().mockResolvedValue([]), GetFileTransferSettings: vi.fn(),
  ListFileTransferHistory: listHistory, AcceptFileTransfer: vi.fn(), CancelFileTransfer: vi.fn(),
  ClearFileTransferHistory: vi.fn(), DeclineFileTransfer: vi.fn(), DiscardPartialFileTransfer: vi.fn(),
  LocateFileTransfer: vi.fn(), OpenFileTransfer: vi.fn(), RemoveFileTransferHistory: vi.fn(),
  RetryFileTransfer: vi.fn(), RevealFileTransfer: vi.fn(),
}));

import { dcc } from '../../../wailsjs/go/models';
import { transferAttentionCount, useFileTransfersStore } from '../file-transfers';

const transfer = (source: Partial<dcc.View> = {}) => dcc.View.createFrom({
  id: 't1', networkId: 1, networkName: 'Libera', peer: 'alice', direction: 'incoming',
  filename: 'photo.jpg', totalBytes: 10, transferredBytes: 0, status: 'offered',
  createdAt: '2026-07-14T10:00:00Z', updatedAt: '2026-07-14T10:00:00Z',
  speedBps: 0, etaSeconds: 0, fileAvailable: false, resumable: false, ...source,
});

describe('file transfers store', () => {
  beforeEach(() => useFileTransfersStore.setState({ live: [], history: [], filter: 'all', search: '', nextCursor: '', error: '' }));

  it('upserts requests and counts only items needing attention', () => {
    useFileTransfersStore.getState().receiveEvent({ type: 'upsert', transfer: transfer() });
    useFileTransfersStore.getState().receiveEvent({ type: 'upsert', transfer: transfer({ id: 't2', status: 'transferring' }) });
    expect(useFileTransfersStore.getState().live).toHaveLength(2);
    expect(transferAttentionCount(useFileTransfersStore.getState().live)).toBe(1);
  });

  it('adds terminal updates to history and responds to reset', () => {
    useFileTransfersStore.getState().receiveEvent({ type: 'upsert', transfer: transfer({ status: 'failed', error: 'Timed out' }) });
    expect(useFileTransfersStore.getState().history[0].status).toBe('failed');
    useFileTransfersStore.getState().receiveEvent({ type: 'reset' });
    expect(useFileTransfersStore.getState().live).toEqual([]);
    expect(useFileTransfersStore.getState().history).toEqual([]);
  });

  it('passes direction and search to paginated history', async () => {
    useFileTransfersStore.setState({ filter: 'incoming', search: 'alice' });
    await useFileTransfersStore.getState().loadHistory();
    expect(listHistory).toHaveBeenCalledWith('incoming', 'alice', '', 50);
  });
});
