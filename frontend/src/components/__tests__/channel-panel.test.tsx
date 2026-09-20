import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
// `storage` must be imported before `ChannelPanel`: Vitest awaits static imports in
// source order, and ChannelPanel statically imports the mocked wailsjs/go/main/App
// module — if that import resolves before `storage` is bound, the vi.mock factory
// below (which references `storage`) throws a TDZ ReferenceError.
import { storage } from '../../../wailsjs/go/models';
import { ChannelPanel } from '../channel-panel';
import { useNetworkStore } from '../../stores/network';

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
  it('marks only the right-clicked channel as read without navigating', async () => {
    const onSelect = vi.fn();
    useNetworkStore.setState({ unreadCounts: new Map([
      ['1:#cascade', 3], ['1:#python', 2], ['1:pm:bob', 1], ['2:#cascade', 4],
    ]) });
    function Panel() {
      const unreadCounts = useNetworkStore((s) => s.unreadCounts);
      return <ChannelPanel network={network} selectedChannel="status" connected
        unreadCounts={unreadCounts} onSelectChannel={onSelect} onShowUserInfo={() => {}} />;
    }
    render(<Panel />);
    const channel = (await screen.findByText(/cascade/)).closest('[data-testid="channel-node"]')!;
    expect(channel).toHaveTextContent('3');
    fireEvent.contextMenu(channel);
    fireEvent.click(await screen.findByRole('button', { name: 'Mark as read' }));

    expect(channel).not.toHaveTextContent('3');
    expect(useNetworkStore.getState().unreadCounts).toEqual(new Map([
      ['1:#python', 2], ['1:pm:bob', 1], ['2:#cascade', 4],
    ]));
    expect(screen.queryByTestId('context-menu')).not.toBeInTheDocument();
    expect(onSelect).not.toHaveBeenCalled();
  });

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
