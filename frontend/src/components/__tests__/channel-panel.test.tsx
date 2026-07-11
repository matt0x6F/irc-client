import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
// `storage` must be imported before `ChannelPanel`: Vitest awaits static imports in
// source order, and ChannelPanel statically imports the mocked wailsjs/go/main/App
// module — if that import resolves before `storage` is bound, the vi.mock factory
// below (which references `storage`) throws a TDZ ReferenceError.
import { storage } from '../../../wailsjs/go/models';
import { ChannelPanel } from '../channel-panel';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetOpenChannels: vi.fn().mockResolvedValue([
    storage.Channel.createFrom({ name: '#cascade' }),
    storage.Channel.createFrom({ name: '#python' }),
  ]),
  GetPrivateMessageConversations: vi.fn().mockResolvedValue(['bob']),
  GetChannels: vi.fn().mockResolvedValue([]),
  GetJoinedChannels: vi.fn().mockResolvedValue([]),
  GetMonitorPresence: vi.fn().mockResolvedValue({}),
  CloseChannel: vi.fn(), LeaveChannel: vi.fn(), SendCommand: vi.fn(),
  SetPrivateMessageOpen: vi.fn(), ClearPaneFocus: vi.fn(),
  ToggleChannelAutoJoin: vi.fn(),
}));
vi.mock('../../../wailsjs/runtime/runtime', () => ({ EventsOn: () => () => {} }));

const network = storage.Network.createFrom({ id: 1, name: 'Libera Chat' });

beforeEach(() => vi.clearAllMocks());

describe('ChannelPanel', () => {
  it('renders the network header, server log, and channels', async () => {
    const onSelect = vi.fn();
    render(
      <ChannelPanel network={network} selectedChannel="status" connected currentNick="nyx_"
        unreadCounts={new Map()} onSelectChannel={onSelect} onShowUserInfo={() => {}} />,
    );
    expect(screen.getByText('Libera Chat')).toBeInTheDocument();
    expect(screen.getByText('Server log')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText(/cascade/)).toBeInTheDocument());
  });

  it('selecting the server log calls onSelectChannel with status', async () => {
    const onSelect = vi.fn();
    render(
      <ChannelPanel network={network} selectedChannel={null} connected currentNick="nyx_"
        unreadCounts={new Map()} onSelectChannel={onSelect} onShowUserInfo={() => {}} />,
    );
    await waitFor(() => expect(screen.getByText(/cascade/)).toBeInTheDocument());
    fireEvent.click(screen.getByText('Server log'));
    expect(onSelect).toHaveBeenCalledWith(1, 'status');
  });
});
