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

describe('/whois interception', () => {
  beforeEach(() => {
    sendCommand.mockClear();
    useUIStore.setState({ showUserInfo: null });
    useNetworkStore.setState({
      selectedNetwork: 1,
      selectedChannel: 'status',
      networks: [{ id: 1, address: 'x', nickname: 'matt0x6f_0' } as never],
    });
  });

  it('/whois <nick> opens the user-info panel and does not hit SendCommand', async () => {
    await useNetworkStore.getState().sendMessage('/whois alice');
    expect(useUIStore.getState().showUserInfo).toEqual({ networkId: 1, nickname: 'alice' });
    // The panel issues its own WHOIS, so we must not double-send.
    expect(sendCommand).not.toHaveBeenCalled();
  });

  it('bare /whois targets your own current nick', async () => {
    await useNetworkStore.getState().sendMessage('/whois');
    expect(useUIStore.getState().showUserInfo).toEqual({ networkId: 1, nickname: 'matt0x6f_0' });
    expect(sendCommand).not.toHaveBeenCalled();
  });
});
