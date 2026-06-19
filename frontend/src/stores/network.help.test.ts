import { describe, it, expect, vi, beforeEach } from 'vitest';

const printLocalLines = vi.fn().mockResolvedValue(undefined);
const sendCommand = vi.fn().mockResolvedValue(undefined);
vi.mock('../../wailsjs/go/main/App', () => ({
  PrintLocalLines: (...a: unknown[]) => printLocalLines(...a),
  SendCommand: (...a: unknown[]) => sendCommand(...a),
  // other App imports used by the store are unused in this test path:
  SendMessage: vi.fn(), SetPaneFocus: vi.fn(), RequestChatHistoryLatest: vi.fn(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

import { useNetworkStore } from './network';
import { useCommandsStore } from './commands';
import { usePreferencesStore } from './preferences';
import { useUIStore } from './ui';

describe('/help interception', () => {
  beforeEach(() => {
    printLocalLines.mockClear(); sendCommand.mockClear();
    useCommandsStore.setState({ commands: [{ name: 'JOIN', aliases: ['J'], category: 'server', usage: '#channel [key]', description: 'Join a channel', source: '' } as never] });
    useNetworkStore.setState({ selectedNetwork: 1, selectedChannel: 'status', networks: [{ id: 1, address: 'x' } as never] });
  });

  it('/help <cmd> prints to the buffer and does not hit SendCommand', async () => {
    await useNetworkStore.getState().sendMessage('/help join');
    expect(printLocalLines).toHaveBeenCalled();
    expect(sendCommand).not.toHaveBeenCalled();
  });

  it('/help with dialog mode opens the dialog', async () => {
    usePreferencesStore.setState({ helpDisplayMode: 'dialog' } as never);
    await useNetworkStore.getState().sendMessage('/help');
    expect(useUIStore.getState().helpOpen).toBe(true);
  });

  it('/help with buffer mode prints the list', async () => {
    usePreferencesStore.setState({ helpDisplayMode: 'buffer' } as never);
    await useNetworkStore.getState().sendMessage('/help');
    expect(printLocalLines).toHaveBeenCalled();
  });
});
