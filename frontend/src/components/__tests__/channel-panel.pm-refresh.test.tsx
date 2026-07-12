import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, act } from '@testing-library/react';
// `storage` must import before `ChannelPanel` (see channel-panel.test.tsx for why).
import { storage } from '../../../wailsjs/go/models';
import { ChannelPanel } from '../channel-panel';

// Capture EventsOn callbacks by event name so a test can fire a synthetic event.
const handlers: Record<string, ((data: unknown) => void)[]> = {};
vi.mock('../../../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (data: unknown) => void) => {
    (handlers[name] ||= []).push(cb);
    return () => {};
  },
}));

const getPm = vi.fn();
vi.mock('../../../wailsjs/go/main/App', () => ({
  GetOpenChannels: vi.fn().mockResolvedValue([]),
  GetPrivateMessageConversations: (...args: unknown[]) => getPm(...args),
  GetChannels: vi.fn().mockResolvedValue([]),
  GetJoinedChannels: vi.fn().mockResolvedValue([]),
  GetMonitorPresence: vi.fn().mockResolvedValue({}),
  CloseChannel: vi.fn(),
  LeaveChannel: vi.fn(),
  SendCommand: vi.fn(),
  SetPrivateMessageOpen: vi.fn(),
  ClearPaneFocus: vi.fn(),
  ToggleChannelAutoJoin: vi.fn(),
}));

const network = storage.Network.createFrom({ id: 1, name: 'e2e' });

function fire(data: unknown) {
  return act(async () => {
    handlers['message-event']?.forEach((cb) => cb(data));
  });
}

function renderPanel() {
  return render(
    <ChannelPanel
      network={network}
      selectedChannel="status"
      connected
      currentNick="e2euser"
      unreadCounts={new Map()}
      onSelectChannel={() => {}}
      onShowUserInfo={() => {}}
    />,
  );
}

beforeEach(() => {
  for (const k of Object.keys(handlers)) delete handlers[k];
  getPm.mockReset();
  getPm.mockResolvedValue([]);
});

describe('ChannelPanel PM refresh on message-event', () => {
  it('refreshes PMs on a PM message-event — a channel-context PM sets `channel` to our own nick, so it is keyed by pmTarget', async () => {
    renderPanel();
    await waitFor(() => expect(getPm).toHaveBeenCalledTimes(1)); // mount load
    // channel-context PM: channel is the recipient (our nick), pmTarget is the peer.
    await fire({
      type: 'message.received',
      data: { networkId: 1, channel: 'e2euser', channelContext: '#test', pmTarget: 'ctxbot', messageType: 'privmsg', user: 'ctxbot' },
    });
    await waitFor(() => expect(getPm).toHaveBeenCalledTimes(2));
    expect(getPm).toHaveBeenLastCalledWith(1, true);
  });

  it('ignores a channel message (no pmTarget) — no extra PM refresh', async () => {
    renderPanel();
    await waitFor(() => expect(getPm).toHaveBeenCalledTimes(1));
    await fire({
      type: 'message.received',
      data: { networkId: 1, channel: '#chan', pmTarget: '', messageType: 'privmsg', user: 'bob' },
    });
    // Give any errant async refresh a chance to run, then assert it did not.
    await new Promise((r) => setTimeout(r, 20));
    expect(getPm).toHaveBeenCalledTimes(1);
  });

  it('ignores a PM for a different network', async () => {
    renderPanel();
    await waitFor(() => expect(getPm).toHaveBeenCalledTimes(1));
    await fire({
      type: 'message.received',
      data: { networkId: 2, channel: 'e2euser', pmTarget: 'ctxbot', messageType: 'privmsg', user: 'ctxbot' },
    });
    await new Promise((r) => setTimeout(r, 20));
    expect(getPm).toHaveBeenCalledTimes(1);
  });
});
