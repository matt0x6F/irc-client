import { describe, it, expect, vi, beforeEach } from 'vitest';

const sendCommand = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  SendCommand: (...a: unknown[]) => sendCommand(...a),
  // other App imports used by the store are unused in this test path:
  PrintLocalLines: vi.fn(), SendMessage: vi.fn(), SetPaneFocus: vi.fn(), RequestChatHistoryLatest: vi.fn(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from './network';
import { useUIStore } from './ui';

describe('/list interception', () => {
  beforeEach(() => {
    sendCommand.mockClear();
    useUIStore.setState({ showChannelList: null });
    useNetworkStore.setState({
      selectedNetwork: 1,
      selectedChannel: 'status',
      networks: [{ id: 1, address: 'x', nickname: 'matt0x6f_0' } as never],
    });
  });

  it('/list opens the browse-channels modal with no filter and does not hit SendCommand', async () => {
    await useNetworkStore.getState().sendMessage('/list');
    expect(useUIStore.getState().showChannelList).toEqual({ networkId: 1 });
    // The modal issues its own RequestChannelList, so we must not double-send.
    expect(sendCommand).not.toHaveBeenCalled();
  });

  it('/list <arg> opens the modal with the arg as a server-side filter', async () => {
    await useNetworkStore.getState().sendMessage('/list >50');
    expect(useUIStore.getState().showChannelList).toEqual({ networkId: 1, filter: '>50' });
    expect(sendCommand).not.toHaveBeenCalled();
  });

  it('/list trims surrounding whitespace from the filter arg', async () => {
    await useNetworkStore.getState().sendMessage('/list   #linux*  ');
    expect(useUIStore.getState().showChannelList).toEqual({ networkId: 1, filter: '#linux*' });
    expect(sendCommand).not.toHaveBeenCalled();
  });
});
