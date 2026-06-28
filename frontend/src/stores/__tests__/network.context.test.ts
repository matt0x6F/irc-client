import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useNetworkStore } from '../network';

// Mock the App bindings so that SendMessageWithContext is a spy we control.
// The shim at wailsjs/go/main/App re-exports from bindings/…/app, so we mock
// at the shim path (same as network.ts imports).
vi.mock('../../../wailsjs/go/main/App', async (orig) => {
  const actual = await (orig() as Promise<Record<string, unknown>>);
  return {
    ...actual,
    SendMessage: vi.fn().mockResolvedValue(undefined),
    SendMessageWithContext: vi.fn().mockResolvedValue(undefined),
    GetChannelIDByName: vi.fn().mockResolvedValue(42),
    GetConnectionStatus: vi.fn(),
    GetMessageByMsgID: vi.fn(),
    GetChannels: vi.fn(),
  };
});

import { SendMessageWithContext } from '../../../wailsjs/go/main/App';

describe('network store: channel-context state', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      replyTarget: null,
      selectedNetwork: null,
      selectedChannel: null,
      networks: [],
      messages: [],
      channelContextByPane: new Map(),
    } as any);
    (SendMessageWithContext as ReturnType<typeof vi.fn>).mockClear();
  });

  it('setChannelContext stores context for a pane', () => {
    useNetworkStore.getState().setChannelContext('pm:bob', '#dev');
    expect(useNetworkStore.getState().channelContextByPane.get('pm:bob')).toBe('#dev');
  });

  it('clearChannelContext removes context for a pane', () => {
    useNetworkStore.getState().setChannelContext('pm:bob', '#dev');
    useNetworkStore.getState().clearChannelContext('pm:bob');
    expect(useNetworkStore.getState().channelContextByPane.get('pm:bob')).toBeUndefined();
  });

  it('clearChannelContext does not affect other panes', () => {
    useNetworkStore.getState().setChannelContext('pm:bob', '#dev');
    useNetworkStore.getState().setChannelContext('pm:alice', '#general');
    useNetworkStore.getState().clearChannelContext('pm:bob');
    expect(useNetworkStore.getState().channelContextByPane.get('pm:alice')).toBe('#general');
    expect(useNetworkStore.getState().channelContextByPane.get('pm:bob')).toBeUndefined();
  });
});

describe('network store: sticky channel-context PM send', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      replyTarget: null,
      selectedNetwork: 1,
      selectedChannel: 'pm:bob',
      networks: [{ id: 1, nickname: 'me' } as any],
      messages: [],
      channelContextByPane: new Map(),
    } as any);
    (SendMessageWithContext as ReturnType<typeof vi.fn>).mockClear();
  });

  it('attaches sticky channel-context to PM sends and persists until cleared', async () => {
    useNetworkStore.getState().setChannelContext('pm:bob', '#dev');
    await useNetworkStore.getState().sendMessage('hi');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, 'bob', 'hi', '', '#dev');

    await useNetworkStore.getState().sendMessage('again');
    expect(SendMessageWithContext).toHaveBeenLastCalledWith(1, 'bob', 'again', '', '#dev'); // still sticky

    useNetworkStore.getState().clearChannelContext('pm:bob');
    await useNetworkStore.getState().sendMessage('plain');
    expect(SendMessageWithContext).toHaveBeenLastCalledWith(1, 'bob', 'plain', '', '');
  });

  it('reply msgid and channel-context are both threaded simultaneously', async () => {
    useNetworkStore.getState().setChannelContext('pm:bob', '#dev');
    useNetworkStore.getState().setReplyTarget({ msgid: 'r1', nick: 'bob', snippet: 'hi' });
    await useNetworkStore.getState().sendMessage('reply');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, 'bob', 'reply', 'r1', '#dev');
    // replyTarget is cleared after send, but context persists
    expect(useNetworkStore.getState().replyTarget).toBeNull();
    expect(useNetworkStore.getState().channelContextByPane.get('pm:bob')).toBe('#dev');
  });

  it('PM send without context uses empty string', async () => {
    await useNetworkStore.getState().sendMessage('no-ctx');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, 'bob', 'no-ctx', '', '');
  });
});

describe('network store: channel pane never gets channel-context', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      replyTarget: null,
      selectedNetwork: 1,
      selectedChannel: '#dev',
      networks: [{ id: 1, nickname: 'me' } as any],
      messages: [],
      channelContextByPane: new Map([['#dev', '#other']]), // even if set, channel pane ignores it
    } as any);
    (SendMessageWithContext as ReturnType<typeof vi.fn>).mockClear();
  });

  it('channel pane always passes empty string as channelContext', async () => {
    await useNetworkStore.getState().sendMessage('hi');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, '#dev', 'hi', '', '');
  });
});
