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
  };
});

import { SendMessageWithContext, SendMessage } from '../../../wailsjs/go/main/App';

describe('network store: reply state', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      replyTarget: null,
      selectedNetwork: null,
      selectedChannel: null,
      networks: [],
      messages: [],
    });
    (SendMessageWithContext as ReturnType<typeof vi.fn>).mockClear();
    (SendMessage as ReturnType<typeof vi.fn>).mockClear();
  });

  it('setReplyTarget stores the target and clearReplyTarget resets it', () => {
    useNetworkStore.getState().setReplyTarget({ msgid: 'p1', nick: 'bob', snippet: 'hello' });
    expect(useNetworkStore.getState().replyTarget).toEqual({ msgid: 'p1', nick: 'bob', snippet: 'hello' });

    useNetworkStore.getState().clearReplyTarget();
    expect(useNetworkStore.getState().replyTarget).toBeNull();
  });
});

describe('network store: reply send wiring (channel)', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      replyTarget: null,
      selectedNetwork: 1,
      selectedChannel: '#dev',
      networks: [{ id: 1, nickname: 'me' } as any],
      messages: [],
    });
    (SendMessageWithContext as ReturnType<typeof vi.fn>).mockClear();
    (SendMessage as ReturnType<typeof vi.fn>).mockClear();
  });

  it('sends with reply msgid as 4th arg and empty channelContext as 5th', async () => {
    useNetworkStore.getState().setReplyTarget({ msgid: 'p1', nick: 'bob', snippet: 'hi' });
    await useNetworkStore.getState().sendMessage('pong');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, '#dev', 'pong', 'p1', '');
    expect(useNetworkStore.getState().replyTarget).toBeNull();
  });

  it('sends with empty reply msgid when no replyTarget', async () => {
    await useNetworkStore.getState().sendMessage('hello world');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, '#dev', 'hello world', '', '');
    expect(SendMessage).not.toHaveBeenCalled();
  });

  it('clears replyTarget after successful send', async () => {
    useNetworkStore.getState().setReplyTarget({ msgid: 'abc', nick: 'alice', snippet: 'test' });
    await useNetworkStore.getState().sendMessage('response');
    expect(useNetworkStore.getState().replyTarget).toBeNull();
  });
});

describe('network store: reply send wiring (PM)', () => {
  beforeEach(() => {
    useNetworkStore.setState({
      replyTarget: null,
      selectedNetwork: 1,
      selectedChannel: 'pm:alice',
      networks: [{ id: 1, nickname: 'me' } as any],
      messages: [],
    });
    (SendMessageWithContext as ReturnType<typeof vi.fn>).mockClear();
    (SendMessage as ReturnType<typeof vi.fn>).mockClear();
  });

  it('PM send uses SendMessageWithContext with reply msgid', async () => {
    useNetworkStore.getState().setReplyTarget({ msgid: 'pm1', nick: 'alice', snippet: 'hi' });
    await useNetworkStore.getState().sendMessage('hey');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, 'alice', 'hey', 'pm1', '');
    expect(useNetworkStore.getState().replyTarget).toBeNull();
  });

  it('PM send uses SendMessageWithContext with empty msgid when no reply', async () => {
    await useNetworkStore.getState().sendMessage('hey');
    expect(SendMessageWithContext).toHaveBeenCalledWith(1, 'alice', 'hey', '', '');
  });
});
