import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', async (orig) => {
  const actual = await (orig() as Promise<Record<string, unknown>>);
  return {
    ...actual,
    GetJoinedChannels: vi.fn(),
    SendCommand: vi.fn().mockResolvedValue(undefined),
    SetPaneFocus: vi.fn().mockResolvedValue(undefined),
  };
});

import { useNetworkStore } from '../network';
import { GetJoinedChannels, SendCommand } from '../../../wailsjs/go/main/App';

describe('network store: openOrJoinChannel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useNetworkStore.setState({ selectedNetwork: null, selectedChannel: null });
  });

  it('switches without joining when already in the channel (case-insensitive)', async () => {
    (GetJoinedChannels as ReturnType<typeof vi.fn>).mockResolvedValue([{ name: '#Test' }]);

    await useNetworkStore.getState().openOrJoinChannel(1, '#test');

    expect(SendCommand).not.toHaveBeenCalled();
    expect(useNetworkStore.getState().selectedChannel).toBe('#test');
    expect(useNetworkStore.getState().selectedNetwork).toBe(1);
  });

  it('joins then switches when not already in the channel', async () => {
    (GetJoinedChannels as ReturnType<typeof vi.fn>).mockResolvedValue([{ name: '#other' }]);

    await useNetworkStore.getState().openOrJoinChannel(1, '#test');

    expect(SendCommand).toHaveBeenCalledWith(1, '/join #test');
    expect(useNetworkStore.getState().selectedChannel).toBe('#test');
  });
});
