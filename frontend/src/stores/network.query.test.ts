import { describe, it, expect, vi, beforeEach } from 'vitest';

const openContact = vi.fn().mockResolvedValue({id:17,reference:'@17',target_user:'alice',presence:'unknown',sessions:[]});
const setPMOpen = vi.fn().mockResolvedValue(undefined);
const setPaneFocus = vi.fn().mockResolvedValue(undefined);
const requestHist = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  OpenPrivateContact: (...a: unknown[]) => openContact(...a),
  SetPrivateMessageOpen: (...a: unknown[]) => setPMOpen(...a),
  SetPaneFocus: (...a: unknown[]) => setPaneFocus(...a),
  RequestChatHistoryLatest: (...a: unknown[]) => requestHist(...a),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from './network';

describe('openQuery', () => {
  beforeEach(() => { setPMOpen.mockClear(); });
  it('opens the conversation and navigates to the pm pane', async () => {
    await useNetworkStore.getState().openQuery(1, 'alice');
    expect(openContact).toHaveBeenCalledWith(1, 'alice');
    expect(useNetworkStore.getState().selectedChannel).toBe('pm:@17');
    expect(useNetworkStore.getState().selectedNetwork).toBe(1);
  });
});
